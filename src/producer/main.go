package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	amqp "github.com/rabbitmq/amqp091-go"
)

type Telemetry struct {
	DeviceID    string      `json:"device_id"`
	SensorType  string      `json:"sensor_type"`
	ReadingType string      `json:"reading_type"`
	Timestamp   string      `json:"timestamp"`
	Value       interface{} `json:"value"`
}

type publisher interface {
	publish(telemetry Telemetry) error
}

type RabbitMQ struct {
	conn    *amqp.Connection
	channel *amqp.Channel
	queue   amqp.Queue
}

func newRabbitMQWithRetry() (*RabbitMQ, error) {
	url := os.Getenv("RABBITMQ_URL")
	if url == "" {
		url = "amqp://guest:guest@localhost:5672/"
	}

	const maxRetries = 10
	const retryDelay = 3 * time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		rmq, err := connect(url)
		if err == nil {
			log.Println("conectado ao RabbitMQ com sucesso")
			return rmq, nil
		}
		log.Printf("tentativa %d/%d falhou: %v — aguardando %s...", attempt, maxRetries, err, retryDelay)
		time.Sleep(retryDelay)
	}

	return nil, fmt.Errorf("não foi possível conectar ao RabbitMQ após %d tentativas", maxRetries)
}

func connect(url string) (*RabbitMQ, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, err
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, err
	}

	q, err := ch.QueueDeclare(
		"telemetry",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, err
	}

	return &RabbitMQ{conn: conn, channel: ch, queue: q}, nil
}

func (r *RabbitMQ) publish(telemetry Telemetry) error {
	body, err := json.Marshal(telemetry)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return r.channel.PublishWithContext(
		ctx,
		"",
		r.queue.Name,
		false,
		false,
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Body:         body,
		},
	)
}

func (r *RabbitMQ) close() {
	if r.channel != nil {
		r.channel.Close()
	}
	if r.conn != nil {
		r.conn.Close()
	}
}

func main() {
	rmq, err := newRabbitMQWithRetry()
	if err != nil {
		log.Fatalf("falha ao conectar no RabbitMQ: %v", err)
	}
	defer rmq.close()

	router := gin.Default()

	router.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})

	router.POST("/telemetry", func(c *gin.Context) {
		receiveTelemetry(c, rmq)
	})

	router.Run(":8080")
}

func receiveTelemetry(c *gin.Context, rmq publisher) {
	var telemetry Telemetry

	if err := c.ShouldBindJSON(&telemetry); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "JSON inválido",
			"details": err.Error(),
		})
		return
	}

	if telemetry.DeviceID == "" ||
		telemetry.Timestamp == "" ||
		telemetry.SensorType == "" ||
		telemetry.ReadingType == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "campos obrigatórios ausentes",
		})
		return
	}

	if telemetry.ReadingType != "analog" && telemetry.ReadingType != "discrete" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "reading_type deve ser 'analog' ou 'discrete'",
		})
		return
	}

	if _, err := time.Parse(time.RFC3339, telemetry.Timestamp); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "timestamp inválido, use formato RFC3339",
		})
		return
	}

	if telemetry.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "campo 'value' é obrigatório",
		})
		return
	}

	if err := rmq.publish(telemetry); err != nil {
		log.Printf("erro ao publicar mensagem no RabbitMQ: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "falha ao enfileirar telemetria",
		})
		return
	}

	log.Printf("telemetria enfileirada: device=%s sensor=%s", telemetry.DeviceID, telemetry.SensorType)

	c.JSON(http.StatusAccepted, gin.H{
		"message": "telemetria recebida e enfileirada com sucesso",
		"data":    telemetry,
	})
}