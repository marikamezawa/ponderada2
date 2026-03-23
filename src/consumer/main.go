package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/lib/pq"
	amqp "github.com/rabbitmq/amqp091-go"
)

type Telemetry struct {
	DeviceID    string      `json:"device_id"`
	Timestamp   string      `json:"timestamp"`
	SensorType  string      `json:"sensor_type"`
	ReadingType string      `json:"reading_type"`
	Value       interface{} `json:"value"`
}

type storer interface {
	save(t Telemetry) error
}

type RabbitMQ struct {
	conn    *amqp.Connection
	channel *amqp.Channel
}

type PostgreSQL struct {
	db *sql.DB
}

func newPostgreSQL() (*PostgreSQL, error) {
	url := os.Getenv("POSTGRES_URL")
	if url == "" {
		url = "postgres://admin:secret@localhost:5432/appdb?sslmode=disable"
	}

	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	log.Println("conectado ao PostgreSQL!")
	return &PostgreSQL{db: db}, nil
}

func (p *PostgreSQL) save(t Telemetry) error {
	valueJSON, err := json.Marshal(t.Value)
	if err != nil {
		return err
	}

	_, err = p.db.Exec(`
		INSERT INTO sensor_readings (device_id, sensor_type, reading_type, timestamp, value)
		VALUES ($1, $2, $3, $4, $5)`,
		t.DeviceID, t.SensorType, t.ReadingType, t.Timestamp, valueJSON,
	)
	return err
}

func newRabbitMQ() (*RabbitMQ, error) {
	url := os.Getenv("RABBITMQ_URL")
	if url == "" {
		url = "amqp://guest:guest@localhost:5672/"
	}

	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, err
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, err
	}

	_, err = ch.QueueDeclare("telemetry", true, false, false, false, nil)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, err
	}

	if err := ch.Qos(1, 0, false); err != nil {
		ch.Close()
		conn.Close()
		return nil, err
	}

	return &RabbitMQ{conn: conn, channel: ch}, nil
}

func (r *RabbitMQ) consume() (<-chan amqp.Delivery, error) {
	return r.channel.Consume("telemetry", "consumer", false, false, false, false, nil)
}

func (r *RabbitMQ) close() {
	if r.channel != nil {
		r.channel.Close()
	}
	if r.conn != nil {
		r.conn.Close()
	}
}

func handleMessage(d amqp.Delivery, pg storer) {
	var telemetry Telemetry

	if err := json.Unmarshal(d.Body, &telemetry); err != nil {
		log.Printf("[ERROR] mensagem inválida, descartando: %v", err)
		d.Nack(false, false)
		return
	}

	if err := pg.save(telemetry); err != nil {
		log.Printf("[ERROR] falha ao salvar no postgres: %v", err)
		d.Nack(false, true)
		return
	}

	log.Printf("[OK] telemetria salva — device=%s sensor=%s reading=%s value=%v timestamp=%s",
		telemetry.DeviceID,
		telemetry.SensorType,
		telemetry.ReadingType,
		telemetry.Value,
		telemetry.Timestamp,
	)

	d.Ack(false)
}

func main() {
	pg, err := newPostgreSQL()
	if err != nil {
		log.Fatalf("falha ao conectar no PostgreSQL: %v", err)
	}
	defer pg.db.Close()

	rmq, err := newRabbitMQ()
	if err != nil {
		log.Fatalf("falha ao conectar no RabbitMQ: %v", err)
	}
	defer rmq.close()

	log.Println("conectado ao RabbitMQ, aguardando mensagens...")

	msgs, err := rmq.consume()
	if err != nil {
		log.Fatalf("falha ao registrar consumer: %v", err)
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case msg, ok := <-msgs:
			if !ok {
				log.Println("canal fechado pelo broker, encerrando...")
				return
			}
			handleMessage(msg, pg)

		case sig := <-quit:
			log.Printf("sinal recebido (%s), encerrando graciosamente...", sig)
			return
		}
	}
}