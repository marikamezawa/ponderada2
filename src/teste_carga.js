import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';

const errorRate = new Rate('error_rate');
const latency = new Trend('latency', true);

export const options = {
  stages: [
    { duration: '30s', target: 10 }, 
    { duration: '1m',  target: 50 },   
    { duration: '1m',  target: 100 }, 
    { duration: '30s', target: 0 }, 
  ],
  thresholds: {
    http_req_duration: ['p(95)<500'], 
    error_rate: ['rate<0.05'],  
  },
};

const DEVICES = [
  'device-001', 'device-002', 'device-003',
  'device-004', 'device-005', 'device-006',
];

const SENSOR_TYPES = ['temperature', 'humidity', 'vibration', 'luminosity', 'pressure'];

const READING_TYPES = ['analog', 'discrete'];

function randomItem(arr) {
  return arr[Math.floor(Math.random() * arr.length)];
}

function randomValue(readingType) {
  if (readingType === 'discrete') {
    return Math.random() > 0.5;
  }
  return parseFloat((Math.random() * 100).toFixed(2));
}

function buildPayload() {
  const readingType = randomItem(READING_TYPES);
  return {
    device_id:    randomItem(DEVICES),
    sensor_type:  randomItem(SENSOR_TYPES),
    reading_type: readingType,
    timestamp:    new Date().toISOString(),
    value:        randomValue(readingType),
  };
}

export default function () {
  const payload = JSON.stringify(buildPayload());

  const params = {
    headers: { 'Content-Type': 'application/json' },
  };

  const res = http.post('http://localhost:8080/telemetry', payload, params);

  const success = check(res, {
    'status 202': (r) => r.status === 202,
    'tempo < 500ms': (r) => r.timings.duration < 500,
  });

  errorRate.add(!success);
  latency.add(res.timings.duration);

  sleep(0.5);
}