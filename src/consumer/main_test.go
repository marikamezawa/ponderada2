package main

import (
	"encoding/json"
	"errors"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
)

// mockPostgreSQL simula o banco sem precisar de conexão real
type mockPostgreSQL struct {
	shouldFail bool
	saved      *Telemetry
}

func (m *mockPostgreSQL) save(t Telemetry) error {
	if m.shouldFail {
		return errors.New("erro simulado no banco")
	}
	m.saved = &t
	return nil
}

// mockDelivery simula uma mensagem do RabbitMQ
type mockDelivery struct {
	acked  bool
	nacked bool
	requeue bool
}

func (m *mockDelivery) toDelivery(body []byte) amqp.Delivery {
	return amqp.Delivery{Body: body}
}

func newDelivery(body []byte) amqp.Delivery {
	return amqp.Delivery{Body: body}
}

func TestHandleMessage_Sucesso(t *testing.T) {
	pg := &mockPostgreSQL{}

	telemetry := Telemetry{
		DeviceID:    "device-001",
		SensorType:  "temperature",
		ReadingType: "analog",
		Timestamp:   "2026-03-17T10:30:00Z",
		Value:       26.7,
	}

	body, _ := json.Marshal(telemetry)
	d := newDelivery(body)

	handleMessage(d, pg)

	assert.NotNil(t, pg.saved)
	assert.Equal(t, "device-001", pg.saved.DeviceID)
	assert.Equal(t, "temperature", pg.saved.SensorType)
	assert.Equal(t, "analog", pg.saved.ReadingType)
}

func TestHandleMessage_JSONInvalido(t *testing.T) {
	pg := &mockPostgreSQL{}

	d := newDelivery([]byte("mensagem corrompida"))

	// não deve salvar nada
	handleMessage(d, pg)

	assert.Nil(t, pg.saved)
}

func TestHandleMessage_FalhaAoSalvar(t *testing.T) {
	pg := &mockPostgreSQL{shouldFail: true}

	telemetry := Telemetry{
		DeviceID:    "device-001",
		SensorType:  "temperature",
		ReadingType: "analog",
		Timestamp:   "2026-03-17T10:30:00Z",
		Value:       26.7,
	}

	body, _ := json.Marshal(telemetry)
	d := newDelivery(body)

	// não deve salvar nada pois o banco falhou
	handleMessage(d, pg)

	assert.Nil(t, pg.saved)
}

func TestHandleMessage_ValorDiscreto(t *testing.T) {
	pg := &mockPostgreSQL{}

	telemetry := Telemetry{
		DeviceID:    "device-002",
		SensorType:  "presence",
		ReadingType: "discrete",
		Timestamp:   "2026-03-17T10:30:00Z",
		Value:       true,
	}

	body, _ := json.Marshal(telemetry)
	d := newDelivery(body)

	handleMessage(d, pg)

	assert.NotNil(t, pg.saved)
	assert.Equal(t, "discrete", pg.saved.ReadingType)
	assert.Equal(t, true, pg.saved.Value)
}

func TestHandleMessage_MensagemVazia(t *testing.T) {
	pg := &mockPostgreSQL{}

	d := newDelivery([]byte(""))

	handleMessage(d, pg)

	assert.Nil(t, pg.saved)
}