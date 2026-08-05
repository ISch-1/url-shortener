// k6 smoke — creates + redirects.
// Usage: k6 run load/create-and-redirect.js
//
// Ramps to 500 VUs and drives two scenarios in parallel:
//   * writers  — POST /api/v1/links at modest rate (bounded by rate limit)
//   * readers  — GET /{code} at high rate against a set of pre-created codes
// Thresholds: p95 redirect < 30 ms, error rate < 1%.

import http from 'k6/http';
import { check, sleep } from 'k6';
import { SharedArray } from 'k6/data';
import { randomIntBetween } from 'https://jslib.k6.io/k6-utils/1.4.0/index.js';

const BASE = __ENV.BASE_URL || 'http://localhost:8080';

// Seed a small set of codes at startup — the redirect path is what we're
// actually stressing. Each VU uses these to avoid hammering the create path
// (which is rate-limited).
const seedCodes = new SharedArray('codes', function () {
    const codes = [];
    for (let i = 0; i < 50; i++) {
        const res = http.post(`${BASE}/api/v1/links`, JSON.stringify({
            originalUrl: `https://example.com/seed-${i}`,
        }), { headers: { 'Content-Type': 'application/json', 'X-Api-Key': `seeder-${i}` }});
        if (res.status === 201) {
            codes.push(res.json('code'));
        }
    }
    return codes;
});

export const options = {
    thresholds: {
        'http_req_failed{scenario:readers}':   ['rate<0.01'],
        'http_req_duration{scenario:readers}': ['p(95)<30'],
    },
    scenarios: {
        readers: {
            executor: 'ramping-vus',
            startVUs: 0,
            stages: [
                { duration: '30s', target: 100 },
                { duration: '2m',  target: 500 },
                { duration: '15s', target: 0   },
            ],
            gracefulRampDown: '5s',
            exec: 'readers',
        },
        writers: {
            executor: 'constant-arrival-rate',
            rate: 5,
            timeUnit: '1s',
            duration: '2m',
            preAllocatedVUs: 5,
            exec: 'writers',
        },
    },
};

export function readers() {
    if (seedCodes.length === 0) return;
    const code = seedCodes[randomIntBetween(0, seedCodes.length - 1)];
    const res = http.get(`${BASE}/${code}`, { redirects: 0 });
    check(res, { 'redirect 301': (r) => r.status === 301 });
    sleep(0.1);
}

export function writers() {
    const res = http.post(`${BASE}/api/v1/links`, JSON.stringify({
        originalUrl: `https://example.com/write-${__ITER}`,
    }), { headers: { 'Content-Type': 'application/json', 'X-Api-Key': 'writer' }});
    check(res, { 'create 201 or 429': (r) => r.status === 201 || r.status === 429 });
}
