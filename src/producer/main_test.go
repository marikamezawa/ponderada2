package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

type mockRabbitMQ struct {
	shouldFail bool
}

func (m *mockRabbitMQ) publish(telemetry Telemetry) error {
	if m.shouldFail {
		return assert.AnError
	}
	return nil
}

func setupRouter(rmq publisher) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/telemetry", func(c *gin.Context) {
		receiveTelemetry(c, rmq)
	})
	return router
}

func TestReceiveTelemetry_Sucesso(t *testing.T) {
	router := setupRouter(&mockRabbitMQ{})

	payload := Telemetry{
		DeviceID:    "device-001",
		SensorType:  "temperature",
		ReadingType: "analog",
		Timestamp:   "2026-03-17T10:30:00Z",
		Value:       26.7,
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/telemetry", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)
}

func TestReceiveTelemetry_JSONInvalido(t *testing.T) {
	router := setupRouter(&mockRabbitMQ{})

	req := httptest.NewRequest(http.MethodPost, "/telemetry", bytes.NewBufferString("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestReceiveTelemetry_CamposAusentes(t *testing.T) {
	router := setupRouter(&mockRabbitMQ{})

	payload := map[string]interface{}{
		"device_id": "device-001",
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/telemetry", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestReceiveTelemetry_ReadingTypeInvalido(t *testing.T) {
	router := setupRouter(&mockRabbitMQ{})

	payload := Telemetry{
		DeviceID:    "device-001",
		SensorType:  "temperature",
		ReadingType: "invalido",
		Timestamp:   "2026-03-17T10:30:00Z",
		Value:       26.7,
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/telemetry", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestReceiveTelemetry_TimestampInvalido(t *testing.T) {
	router := setupRouter(&mockRabbitMQ{})

	payload := Telemetry{
		DeviceID:    "device-001",
		SensorType:  "temperature",
		ReadingType: "analog",
		Timestamp:   "17/03/2026",
		Value:       26.7,
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/telemetry", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestReceiveTelemetry_ValueNil(t *testing.T) {
	router := setupRouter(&mockRabbitMQ{})

	payload := map[string]interface{}{
		"device_id":    "device-001",
		"sensor_type":  "temperature",
		"reading_type": "analog",
		"timestamp":    "2026-03-17T10:30:00Z",
		"value":        nil,
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/telemetry", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestReceiveTelemetry_ReadingTypeDiscrete(t *testing.T) {
	router := setupRouter(&mockRabbitMQ{})

	payload := Telemetry{
		DeviceID:    "device-002",
		SensorType:  "presence",
		ReadingType: "discrete",
		Timestamp:   "2026-03-17T10:30:00Z",
		Value:       true,
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/telemetry", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)
}

func TestReceiveTelemetry_FalhaNoRabbitMQ(t *testing.T) {
	router := setupRouter(&mockRabbitMQ{shouldFail: true})

	payload := Telemetry{
		DeviceID:    "device-001",
		SensorType:  "temperature",
		ReadingType: "analog",
		Timestamp:   "2026-03-17T10:30:00Z",
		Value:       26.7,
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/telemetry", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}