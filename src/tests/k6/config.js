import exec from 'k6/execution';

const durations = (__ENV.SCHAKKER_K6_STAGE_SECONDS || '10,15,20,30').split(',').map(Number);
if (durations.length !== 4 || durations.some((n) => !Number.isFinite(n) || n < 1)) throw new Error('Provide four positive stage durations');
export const phases = ['normal', 'load', 'stress', 'recovery'];
export const stageSeconds = durations;
export const fixture = JSON.parse(open(__ENV.SCHAKKER_K6_FIXTURE || './fixture.json'));
if (!/^http:\/\/127\.0\.0\.1:\d+$/.test(fixture.base_url)) throw new Error('The disposable fixture must use a loopback URL');
export function phase() {
  let until = 0;
  const elapsed = exec.instance.currentTestRunDuration / 1000;
  for (let i = 0; i < durations.length; i++) { until += durations[i]; if (elapsed < until) return phases[i]; }
  return 'recovery';
}
export function stages(targets) {
  const rampDown = Math.min(10, durations[3] / 2);
  return [
    ...targets.slice(0, 3).map((target, i) => ({ duration: `${durations[i]}s`, target })),
    { duration: `${rampDown}s`, target: targets[3] },
    { duration: `${durations[3] - rampDown}s`, target: targets[3] },
  ];
}
export const thresholds = {
  api_latency_ms: [`p(95)<${__ENV.SCHAKKER_K6_API_P95_MS || 750}`],
  api_errors: [`rate<${__ENV.SCHAKKER_K6_API_ERROR_RATE || 0.01}`],
  job_failures: ['rate<0.01'],
  jobs_completed: ['count>0'],
};
for (const name of phases) {
  // Per-stage submetrics make p50/p95, errors and completion rates reproducible.
  thresholds[`api_latency_ms{phase:${name}}`] = ['p(95)<10000'];
  thresholds[`api_requests{phase:${name}}`] = ['count>=0'];
  thresholds[`api_errors{phase:${name}}`] = ['rate<1'];
  thresholds[`jobs_completed{phase:${name}}`] = ['count>=0'];
  thresholds[`queue_wait_ms{phase:${name}}`] = ['p(95)<120000'];
  thresholds[`migration_duration_ms{phase:${name}}`] = ['p(95)<120000'];
}
