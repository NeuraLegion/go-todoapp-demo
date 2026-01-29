import { test, before, after } from 'node:test';
import { SecRunner } from '@sectester/runner';
import { AttackParamLocation, HttpMethod } from '@sectester/scan';

const timeout = 40 * 60 * 1000;
const baseUrl = process.env.BRIGHT_TARGET_URL!;

let runner!: SecRunner;

before(async () => {
  runner = new SecRunner({
    hostname: process.env.BRIGHT_HOSTNAME!,
    projectId: process.env.BRIGHT_PROJECT_ID!
  });

  await runner.init();
});

after(() => runner.clear());

test('PUT /todo/:id', { signal: AbortSignal.timeout(timeout) }, async () => {
  await runner
    .createScan({
      tests: ['sqli', 'csrf', 'bopla', 'xss'],
      attackParamLocations: [AttackParamLocation.BODY, AttackParamLocation.PATH],
      starMetadata: {
        code_source: 'NeuraLegion/go-todoapp-demo:main',
        databases: ['SQLite'],
        user_roles: []
      },
      poolSize: +process.env.SECTESTER_SCAN_POOL_SIZE || undefined
    })
    .setFailFast(false)
    .timeout(timeout)
    .run({
      method: HttpMethod.PUT,
      url: `${baseUrl}/todo/123e4567-e89b-12d3-a456-426614174000`,
      body: {
        title: 'Updated Todo Title',
        completed: true
      },
      headers: { 'Content-Type': 'application/json' }
    });
});