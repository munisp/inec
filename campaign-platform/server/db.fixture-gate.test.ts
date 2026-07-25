import { afterEach, describe, expect, it } from "vitest";
import { isFixtureSeedingAllowed } from "./db";

const originalNodeEnv = process.env.NODE_ENV;
const originalFixtureFlag = process.env.CAMPAIGN_ALLOW_FIXTURE_SEED;
const originalGithubActions = process.env.GITHUB_ACTIONS;

function restore(name: "NODE_ENV" | "CAMPAIGN_ALLOW_FIXTURE_SEED" | "GITHUB_ACTIONS", value: string | undefined) {
  if (value === undefined) delete process.env[name];
  else process.env[name] = value;
}

afterEach(() => {
  restore("NODE_ENV", originalNodeEnv);
  restore("CAMPAIGN_ALLOW_FIXTURE_SEED", originalFixtureFlag);
  restore("GITHUB_ACTIONS", originalGithubActions);
});

describe("isFixtureSeedingAllowed", () => {
  it("rejects fixture seeding in production even when explicitly requested", () => {
    process.env.NODE_ENV = "production";
    process.env.CAMPAIGN_ALLOW_FIXTURE_SEED = "true";
    delete process.env.GITHUB_ACTIONS;

    expect(isFixtureSeedingAllowed()).toBe(false);
  });

  it("requires an explicit fixture flag in non-production", () => {
    process.env.NODE_ENV = "test";
    delete process.env.CAMPAIGN_ALLOW_FIXTURE_SEED;
    delete process.env.GITHUB_ACTIONS;

    expect(isFixtureSeedingAllowed()).toBe(false);
  });

  it("permits fixture seeding only in an explicitly enabled test runtime", () => {
    process.env.NODE_ENV = "test";
    process.env.CAMPAIGN_ALLOW_FIXTURE_SEED = "true";
    delete process.env.GITHUB_ACTIONS;

    expect(isFixtureSeedingAllowed()).toBe(true);
  });

  it("permits an explicitly enabled GitHub Actions fixture runtime without making production eligible", () => {
    process.env.NODE_ENV = "production";
    process.env.CAMPAIGN_ALLOW_FIXTURE_SEED = "true";
    process.env.GITHUB_ACTIONS = "true";

    expect(isFixtureSeedingAllowed()).toBe(true);
  });
});
