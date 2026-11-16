import http from 'k6/http';
import { Counter, Rate, Trend } from 'k6/metrics';
import { sleep } from 'k6';
import { fixture, phase } from './config.js';

export const apiLatency = new Trend('api_latency_ms', true);
export const apiRequests = new Counter('api_requests');
export const apiErrors = new Rate('api_errors');
export const jobsAccepted = new Counter('jobs_accepted');
export const jobsCompleted = new Counter('jobs_completed');
export const jobFailures = new Rate('job_failures');
export const queueWait = new Trend('queue_wait_ms', true);
export const migrationDuration = new Trend('migration_duration_ms', true);
export const jobCompletion = new Trend('job_completion_ms', true);
export const fixtureContention = new Counter('fixture_target_contention');

export function record(response, tags) {
  apiLatency.add(response.timings.duration, tags);
  apiRequests.add(1, tags);
  apiErrors.add(response.status < 200 || response.status >= 400, tags);
  return response;
}
export function parameters(data, name, tags = {}) {
  return { headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': data.csrf, Origin: fixture.base_url }, tags: { name, ...tags }, timeout: '15s' };
}
export function installSession(data) {
  http.cookieJar().set(fixture.base_url, 'schakker_session', data.cookie);
}
export function login() {
  const challenge = http.get(`${fixture.base_url}/api/session`, { tags: { name: 'session_bootstrap' } }).json();
  const response = http.post(`${fixture.base_url}/api/login`, JSON.stringify({ username: fixture.username, password: fixture.password }), parameters({ csrf: challenge.CSRF }, 'login'));
  if (response.status !== 200) throw new Error(`Fixture login failed: ${response.status}`);
  const session = http.get(`${fixture.base_url}/api/session`, { tags: { name: 'session' } }).json();
  return { csrf: session.CSRF, cookie: http.cookieJar().cookiesForURL(fixture.base_url).schakker_session[0] };
}
export function pollJob(id, data, tags) {
  const started = Date.now();
  const timeout = Number(__ENV.SCHAKKER_K6_JOB_TIMEOUT_SECONDS || 60) * 1000;
  while (Date.now() - started < timeout) {
    const response = record(http.get(`${fixture.base_url}/api/migration-jobs/${id}`, parameters(data, 'job_status', tags)), tags);
    if (response.status !== 200) { jobFailures.add(true, tags); return; }
    const job = response.json().job;
    if (job.status === 'succeeded' || job.status === 'reverted') {
      const completedTags = { ...tags, phase: phase() };
      // One job completion, even when enqueue was delivered concurrently twice.
      jobsCompleted.add(1, completedTags); jobFailures.add(false, completedTags);
      jobCompletion.add(Date.now() - started, completedTags);
      if (job.started_at) queueWait.add(Math.max(0, Date.parse(job.started_at) - Date.parse(job.created_at)), completedTags);
      if (job.started_at && job.completed_at) migrationDuration.add(Math.max(0, Date.parse(job.completed_at) - Date.parse(job.started_at)), completedTags);
      return;
    }
    if (['failed', 'partial', 'uncertain', 'partially_reverted'].includes(job.status) && !job.next_retry_at) { jobFailures.add(true, tags); return; }
    sleep(0.1);
  }
  jobFailures.add(true, tags);
}
export function currentTags(workload) { return { phase: phase(), workload }; }
