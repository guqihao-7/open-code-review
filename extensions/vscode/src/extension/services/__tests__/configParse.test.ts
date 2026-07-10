// src/extension/services/__tests__/configParse.test.ts
import { parseConfig, toConfigSetArgs } from '../configParse';

describe('parseConfig', () => {
  it('完整 config 转 camelCase', () => {
    const raw = JSON.stringify({
      provider: 'anthropic',
      providers: {
        anthropic: { api_key: 'k', model: 'claude-opus-4-6', models: ['claude-opus-4-6'] },
      },
      custom_providers: {
        'my-llm': { url: 'https://x', protocol: 'openai', model: 'm', api_key: 'k2' },
      },
      llm: { url: 'u', auth_token: 't', model: 'm', use_anthropic: true, auth_header: 'x-api-key' },
      language: 'Chinese',
    });
    expect(parseConfig(raw)).toEqual({
      provider: 'anthropic',
      model: '',
      providers: {
        anthropic: { apiKey: 'k', url: '', protocol: '', model: 'claude-opus-4-6', models: ['claude-opus-4-6'], authHeader: '' },
      },
      customProviders: {
        'my-llm': { apiKey: 'k2', url: 'https://x', protocol: 'openai', model: 'm', authHeader: '' },
      },
      llm: { url: 'u', authToken: 't', model: 'm', useAnthropic: true, authHeader: 'x-api-key' },
      language: 'Chinese',
    });
  });

  it('缺字段时给默认值', () => {
    const cfg = parseConfig('{}');
    expect(cfg?.llm.url).toBe('');
    expect(cfg?.llm.useAnthropic).toBe(true);
    expect(cfg?.providers).toEqual({});
    expect(cfg?.customProviders).toEqual({});
    expect(cfg?.language).toBe('Chinese');
  });

  it('空字符串 → null', () => {
    expect(parseConfig('')).toBeNull();
  });

  it('providers 为数组时忽略', () => {
    const cfg = parseConfig(JSON.stringify({ providers: ['bad'], custom_providers: [] }));
    expect(cfg?.providers).toEqual({});
    expect(cfg?.customProviders).toEqual({});
  });

  it('解析 exec provider 字段', () => {
    const cfg = parseConfig(JSON.stringify({
      provider: 'codex-cli',
      custom_providers: {
        'codex-cli': {
          transport: 'exec',
          command: 'codex',
          args: ['exec', '-'],
          env: ['CODEX_HOME=/tmp/codex'],
          model: 'default',
          timeout_sec: 300,
          max_concurrency: 1,
        },
      },
    }));
    expect(cfg?.customProviders['codex-cli']).toMatchObject({
      transport: 'exec',
      command: 'codex',
      args: ['exec', '-'],
      env: ['CODEX_HOME=/tmp/codex'],
      model: 'default',
      timeoutSec: 300,
      maxConcurrency: 1,
    });
  });
});

describe('toConfigSetArgs', () => {
  it('生成 config set 参数', () => {
    expect(toConfigSetArgs('llm.model', 'opus')).toEqual(['config', 'set', 'llm.model', 'opus']);
    expect(toConfigSetArgs('providers.anthropic.api_key', 'sk')).toEqual(['config', 'set', 'providers.anthropic.api_key', 'sk']);
  });
});
