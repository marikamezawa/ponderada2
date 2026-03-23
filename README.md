# Industrial Telemetry — Processamento Assíncrono de Sensores

Backend de telemetria industrial construído em **Go**, com arquitetura desacoplada via **RabbitMQ** e persistência em **PostgreSQL**, totalmente conteinerizado com **Docker Compose**.

---

## Visão Geral

O sistema recebe pacotes de telemetria enviados por dispositivos embarcados via HTTP e os processa de forma **assíncrona**. O endpoint de ingestão publica as mensagens em uma fila RabbitMQ imediatamente, retornando `202 Accepted` ao dispositivo sem aguardar persistência — eliminando gargalos e garantindo resiliência sob alta carga.

Um serviço consumidor independente consome as mensagens da fila e as persiste no banco de dados PostgreSQL.

---

## Arquitetura

```
┌──────────────────┐        POST /telemetry        ┌─────────────────────┐
│ Dispositivo       │ ──────────────────────────────▶│   Producer (Go/Gin) │
│ Embarcado        │                                 │   porta 8080        │
└──────────────────┘                                 └──────────┬──────────┘
                                                                │
                                                   publish (AMQP, durable)
                                                                │
                                                                ▼
                                                    ┌───────────────────────┐
                                                    │   RabbitMQ            │
                                                    │   fila: telemetry     │
                                                    │   porta 5672          │
                                                    │   mgmt:  15672        │
                                                    └───────────┬───────────┘
                                                                │
                                                     consume (ack manual)
                                                                │
                                                                ▼
                                                    ┌───────────────────────┐
                                                    │   Consumer (Go)       │
                                                    │   prefetch: 1         │
                                                    └───────────┬───────────┘
                                                                │
                                                          INSERT SQL
                                                                │
                                                                ▼
                                                    ┌───────────────────────┐
                                                    │   PostgreSQL 16       │
                                                    │   tabela:             │
                                                    │   sensor_readings     │
                                                    │   porta 5432          │
                                                    └───────────────────────┘
```

### Decisões de Design

| Decisão | Justificativa |
|---|---|
| **Producer retorna 202 sem aguardar persistência** | Desacopla latência de I/O de banco da resposta HTTP, permitindo absorção de picos |
| **Mensagens duráveis no RabbitMQ** (`durable: true`, `DeliveryMode: Persistent`) | Garante que mensagens não sejam perdidas em caso de reinicialização do broker |
| **Ack manual no Consumer com `prefetch: 1`** | Evita perda de mensagens em caso de falha no processamento; mensagens com erro são reenfileiradas |
| **Retry com backoff no Producer** | Reconnecta automaticamente ao RabbitMQ (até 10 tentativas, intervalo de 3s) durante inicialização do container |
| **Coluna `value` como JSONB** | Suporta tanto leituras analógicas (float) quanto discretas (boolean) sem schema fixo |
| **Imagens Docker multi-stage** | Binários finais em Alpine (~10 MB), sem toolchain Go na imagem de produção |

---

## Estrutura do Projeto

```
src/
├── docker-compose.yaml       # Orquestração dos 4 serviços
├── .env                      # Variáveis de ambiente (credenciais)
├── Makefile                  # Atalhos: up / down
├── teste_carga.js            # Script k6 para teste de carga
│
├── producer/                 # Serviço HTTP — recebe e enfileira telemetria
│   ├── main.go               # Handler HTTP + publicação RabbitMQ
│   ├── main_test.go          # Testes unitários com mock do RabbitMQ
│   ├── Dockerfile            # Build multi-stage
│   ├── go.mod / go.sum
│   └── Makefile
│
└── consumer/                 # Serviço worker — consome fila e persiste
    ├── main.go               # Loop de consumo + persistência PostgreSQL
    ├── main_test.go          # Testes unitários com mock do PostgreSQL
    ├── Dockerfile            # Build multi-stage
    ├── go.mod / go.sum
    └── Makefile
```

---

## Modelo de Dados

A tabela `sensor_readings` foi projetada para suportar leituras heterogêneas (analógicas e discretas) de múltiplos tipos de sensores, sem necessidade de tabelas separadas por tipo.

```sql
CREATE TABLE IF NOT EXISTS sensor_readings (
    id           BIGSERIAL PRIMARY KEY,
    device_id    TEXT        NOT NULL,
    sensor_type  TEXT        NOT NULL,
    reading_type TEXT        NOT NULL CHECK (reading_type IN ('analog', 'discrete')),
    timestamp    TIMESTAMPTZ NOT NULL,
    value        JSONB       NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

**Exemplos de registros:**

| device_id | sensor_type | reading_type | value | timestamp |
|---|---|---|---|---|
| device-001 | temperature | analog | `26.7` | 2026-03-17T10:30:00Z |
| device-002 | presence | discrete | `true` | 2026-03-17T10:30:01Z |
| device-003 | humidity | analog | `61.3` | 2026-03-17T10:30:02Z |

---

## API

### `POST /telemetry`

Recebe um pacote de telemetria de um dispositivo embarcado e o enfileira no RabbitMQ.

**Request Body:**

```json
{
  "device_id":    "device-001",
  "sensor_type":  "temperature",
  "reading_type": "analog",
  "timestamp":    "2026-03-17T10:30:00Z",
  "value":        26.7
}
```

| Campo | Tipo | Obrigatório | Descrição |
|---|---|---|---|
| `device_id` | string | ✅ | Identificador único do dispositivo |
| `sensor_type` | string | ✅ | Tipo do sensor (`temperature`, `humidity`, `vibration`, etc.) |
| `reading_type` | string | ✅ | `"analog"` ou `"discrete"` |
| `timestamp` | string | ✅ | Data/hora em formato RFC3339 |
| `value` | any | ✅ | Float para analógico, boolean para discreto |

**Respostas:**

| Status | Descrição |
|---|---|
| `202 Accepted` | Telemetria enfileirada com sucesso |
| `400 Bad Request` | JSON inválido, campos ausentes, `reading_type` inválido ou timestamp fora do formato RFC3339 |
| `500 Internal Server Error` | Falha ao publicar no RabbitMQ |

**Exemplo de resposta `202`:**

```json
{
  "message": "telemetria recebida e enfileirada com sucesso",
  "data": {
    "device_id": "device-001",
    "sensor_type": "temperature",
    "reading_type": "analog",
    "timestamp": "2026-03-17T10:30:00Z",
    "value": 26.7
  }
}
```

---

### `GET /ping`

Health check simples.

```json
{ "message": "pong" }
```

---

## Como Rodar

### Pré-requisitos

- [Docker](https://docs.docker.com/get-docker/) ≥ 24
- [Docker Compose](https://docs.docker.com/compose/) ≥ 2 (incluso no Docker Desktop)

### 1. Clone o repositório e acesse o diretório

```bash
git clone <url-do-repositorio>
cd src/
```

### 2. (Opcional) Ajuste as variáveis de ambiente

As credenciais padrão já estão configuradas no `.env`. Edite o arquivo caso queira personalizá-las:

```bash
# src/.env
POSTGRES_USER=admin
POSTGRES_PASSWORD=secret
POSTGRES_DB=appdb

RABBITMQ_USER=admin
RABBITMQ_PASSWORD=secret
RABBITMQ_VHOST=/
```

### 3. Suba todos os serviços

```bash
make up
# equivalente a: docker compose up -d
```

O Docker Compose irá:
1. Iniciar o **PostgreSQL** e aguardar seu healthcheck
2. Iniciar o **RabbitMQ** e aguardar seu healthcheck
3. Buildar e iniciar o **Producer** (aguarda RabbitMQ saudável)
4. Buildar e iniciar o **Consumer** (aguarda RabbitMQ e PostgreSQL saudáveis)

### 4. Verifique os serviços

```bash
# Health check do producer
curl http://localhost:8080/ping
# → {"message":"pong"}

# Painel de administração do RabbitMQ
# Acesse: http://localhost:15672
# Usuário: admin | Senha: secret
```

### 5. Envie uma telemetria de teste

```bash
curl -X POST http://localhost:8080/telemetry \
  -H "Content-Type: application/json" \
  -d '{
    "device_id":    "device-001",
    "sensor_type":  "temperature",
    "reading_type": "analog",
    "timestamp":    "2026-03-17T10:30:00Z",
    "value":        26.7
  }'
```

### 6. Encerre o ambiente

```bash
make down
# equivalente a: docker compose down
```

---

## Testes Unitários

Os testes são independentes de infraestrutura — utilizam mocks para RabbitMQ e PostgreSQL.

```bash
# Testes do Producer
cd src/producer
go test ./... -v

# Testes do Consumer
cd src/consumer
go test ./... -v
```

**Cobertura dos testes:**

| Serviço | Cenários testados |
|---|---|
| **Producer** | Sucesso (`202`), JSON inválido, campos ausentes, `reading_type` inválido, timestamp inválido, `value` nulo, leitura discreta, falha no RabbitMQ |
| **Consumer** | Persistência com sucesso, JSON corrompido, falha no banco (requeue), leitura discreta (boolean), mensagem vazia |

---

## Teste de Carga com k6

### Pré-requisitos

- [k6](https://k6.io/docs/getting-started/installation/) instalado localmente

### Executar

Com o ambiente rodando (`make up`), execute na pasta `src/`:

```bash
k6 run teste_carga.js
```

### Perfil de carga

O script simula **6 dispositivos** enviando leituras de **5 tipos de sensores** com tipos de leitura aleatórios (analógico/discreto), seguindo o ramp-up:

| Estágio | Duração | VUs |
|---|---|---|
| Aquecimento | 30s | 0 → 10 |
| Carga moderada | 1min | 10 → 50 |
| Carga máxima | 1min | 50 → 100 |
| Rampa de descida | 30s | 100 → 0 |

**Thresholds definidos:**

- Taxa de erros < 5%
- Latência p(95) < 500ms

---

## Resultados do Teste de Carga

```
scenarios: 1 cenário, 100 VUs máx, duração 3m0s (4 estágios)

THRESHOLDS
  error_rate            ✓ rate < 0.05    →  0.00%
  http_req_duration     ✓ p(95) < 500ms  →  p(95) = 4.53ms

TOTAL RESULTS
  Requisições totais     15.716
  Throughput             87.2 req/s
  Taxa de erro           0.00%  (0 / 15.716)
  Checks aprovados       100%   (31.432 / 31.432)

LATÊNCIA (http_req_duration)
  avg    2.81ms
  min    1.29ms
  med    2.48ms
  p(90)  3.72ms
  p(95)  4.53ms
  max   52.79ms
```

### Análise dos Resultados

**✅ Desempenho excelente sob carga.** O sistema processou 15.716 requisições em 3 minutos com 100 usuários virtuais simultâneos, atingindo **zero erros** e latência mediana de apenas **2,48ms** — muito abaixo do threshold de 500ms.

O p(95) de **4,53ms** demonstra consistência: 95% das requisições foram respondidas em menos de 5ms, indicando que o endpoint de ingestão não sofre degradação significativa nem sob carga máxima de 100 VUs.

**Comportamento esperado do endpoint:** O retorno rápido (`202 Accepted`) é consequência direta da arquitetura assíncrona — o Producer apenas serializa o payload e publica na fila, sem aguardar I/O de banco. O peso do processamento é descarregado no Consumer, que opera independentemente.

**Possíveis gargalos em escala maior:** Em cenários com volume muito superior (ex.: 1.000+ VUs), o gargalo tenderia a migrar para o Consumer, que opera com `prefetch: 1`. Escalar horizontalmente o Consumer (múltiplas instâncias consumindo a mesma fila) seria a estratégia natural para aumentar a capacidade de processamento sem alteração no Producer.

---

## Variáveis de Ambiente

| Variável | Serviço | Padrão | Descrição |
|---|---|---|---|
| `RABBITMQ_URL` | Producer, Consumer | `amqp://guest:guest@localhost:5672/` | URL de conexão AMQP |
| `POSTGRES_URL` | Consumer | `postgres://admin:secret@localhost:5432/appdb?sslmode=disable` | URL de conexão PostgreSQL |
| `POSTGRES_USER` | PostgreSQL | `admin` | Usuário do banco |
| `POSTGRES_PASSWORD` | PostgreSQL | `secret` | Senha do banco |
| `POSTGRES_DB` | PostgreSQL | `appdb` | Nome do banco de dados |
| `RABBITMQ_USER` | RabbitMQ | `admin` | Usuário do broker |
| `RABBITMQ_PASSWORD` | RabbitMQ | `secret` | Senha do broker |
| `RABBITMQ_VHOST` | RabbitMQ | `/` | Virtual host |