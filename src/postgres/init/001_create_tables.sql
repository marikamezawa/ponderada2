-- Migration: criação inicial das tabelas
-- Executado automaticamente pelo PostgreSQL na primeira inicialização

CREATE TABLE IF NOT EXISTS sensor_readings (
    id            BIGSERIAL PRIMARY KEY,
    device_id     VARCHAR(255)             NOT NULL,
    sensor_type   VARCHAR(100)             NOT NULL,
    reading_type  VARCHAR(50)              NOT NULL CHECK (reading_type IN ('analog', 'discrete')),
    timestamp     TIMESTAMPTZ              NOT NULL,
    value         JSONB                    NOT NULL,
    created_at    TIMESTAMPTZ              NOT NULL DEFAULT NOW()
);

-- Índices para consultas comuns
CREATE INDEX IF NOT EXISTS idx_sensor_readings_device_id   ON sensor_readings (device_id);
CREATE INDEX IF NOT EXISTS idx_sensor_readings_sensor_type  ON sensor_readings (sensor_type);
CREATE INDEX IF NOT EXISTS idx_sensor_readings_timestamp    ON sensor_readings (timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_sensor_readings_device_ts    ON sensor_readings (device_id, timestamp DESC);
