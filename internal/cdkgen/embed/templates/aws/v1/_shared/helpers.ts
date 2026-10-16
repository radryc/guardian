import * as cdk from 'aws-cdk-lib';
import * as ecs from 'aws-cdk-lib/aws-ecs';
import * as ssm from 'aws-cdk-lib/aws-ssm';
import { Construct } from 'constructs';

/**
 * Shared helpers for Guardian asset templates.
 *
 * Every template module exports a factory function that creates resources
 * directly on the stack scope with stable construct IDs. The Guardian
 * composer (guardian/internal/cdkgen) copies exactly the selected template
 * files plus this directory into the generated stack payload.
 *
 * Keep this file dependency-free apart from aws-cdk-lib/constructs.
 */

export interface AssetCall {
  properties: Record<string, any>;
  generated: Record<string, any>;
  refs: Record<string, any>;
  partition: string;
  intent: string;
}

export interface TemplateHandle {
  [key: string]: any;
}

export function outputLogicalId(assetName: string, key: string): string {
  return `Out${titleParts(assetName)}${titleParts(key)}`;
}

export function exportOutput(stack: cdk.Stack, assetName: string, key: string, value: string): void {
  new cdk.CfnOutput(stack, outputLogicalId(assetName, key), { value });
}

export function titleParts(value: string): string {
  let out = '';
  let upperNext = true;
  for (const ch of value) {
    const isAlphaNum = (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9');
    if (isAlphaNum) {
      out += upperNext && ch >= 'a' && ch <= 'z' ? ch.toUpperCase() : ch;
      upperNext = false;
    } else {
      upperNext = true;
    }
  }
  return out;
}

export function constructId(value: string): string {
  const parts = value.split(/[^A-Za-z0-9]+/).filter((part) => part.length > 0);
  return parts.map((part) => part.charAt(0).toUpperCase() + part.slice(1)).join('') || 'Asset';
}

export function boolValue(value: any): boolean {
  return value === true || value === 'true';
}

export function stringList(value: any): string[] {
  if (!Array.isArray(value)) {
    return [];
  }
  return value.filter((item) => typeof item === 'string' && item.trim() !== '');
}

export function objectList(value: any): Record<string, any>[] {
  if (!Array.isArray(value)) {
    return [];
  }
  return value.filter((item) => item && typeof item === 'object');
}

export function firstPort(ports: any): number {
  for (const port of objectList(ports)) {
    if (port.containerPort) {
      return Number(port.containerPort);
    }
    if (port.port) {
      return Number(port.port);
    }
    if (port.servicePort) {
      return Number(port.servicePort);
    }
  }
  return 0;
}

export function buildPortMappings(ports: any): ecs.PortMapping[] {
  const mappings: ecs.PortMapping[] = [];
  objectList(ports).forEach((port, index) => {
    const containerPort = Number(port.containerPort ?? port.port ?? port.servicePort ?? 0);
    if (!containerPort) {
      return;
    }
    const protocol = String(port.protocol || 'tcp').toLowerCase() === 'udp' ? ecs.Protocol.UDP : ecs.Protocol.TCP;
    mappings.push({
      containerPort,
      protocol,
      name: String(port.name || `port-${index}`),
    });
  });
  return mappings;
}

export function buildEnvironment(env: any, partition: string, intent: string, asset: string): Record<string, string> {
  const out: Record<string, string> = {};
  if (env && typeof env === 'object') {
    for (const [key, value] of Object.entries(env)) {
      out[key] = value === null || value === undefined ? '' : String(value);
    }
  }
  let attrs = `guardian.partition=${partition},guardian.intent=${intent},guardian.asset=${asset}`;
  if (out.OTEL_RESOURCE_ATTRIBUTES) {
    attrs += `,${out.OTEL_RESOURCE_ATTRIBUTES}`;
  }
  out.OTEL_RESOURCE_ATTRIBUTES = attrs;
  return out;
}

export function buildSecrets(scope: Construct, asset: string, configParams: any): Record<string, ecs.Secret> {
  const secrets: Record<string, ecs.Secret> = {};
  if (configParams && typeof configParams === 'object') {
    for (const [configName, paramName] of Object.entries(configParams)) {
      const parameter = ssm.StringParameter.fromStringParameterName(
        scope,
        `Param${constructId(asset)}${constructId(configName)}`,
        String(paramName),
      );
      secrets[configName] = ecs.Secret.fromSsmParameter(parameter);
    }
  }
  return secrets;
}

export function buildHealthCheck(spec: any): ecs.HealthCheck | undefined {
  if (!spec || typeof spec !== 'object') {
    return undefined;
  }
  const command = stringList(spec.test);
  if (command.length === 0) {
    return undefined;
  }
  return {
    command,
    interval: durationOrUndefined(spec.interval),
    timeout: durationOrUndefined(spec.timeout),
    retries: spec.retries !== undefined ? Number(spec.retries) : undefined,
  };
}

export function durationOrUndefined(value: any): cdk.Duration | undefined {
  const raw = String(value ?? '').trim();
  if (!raw) {
    return undefined;
  }
  try {
    return cdk.Duration.parse(raw);
  } catch {
    return undefined;
  }
}

export function resolveResources(resources: any): { cpu: number; memory: number } {
  let cpu = 256;
  let memory = 512;
  if (resources && typeof resources === 'object') {
    const limits = resources.limits || {};
    const requests = resources.requests || {};
    if (limits.cpu) {
      cpu = parseCPU(limits.cpu);
    } else if (requests.cpu) {
      cpu = parseCPU(requests.cpu);
    }
    if (limits.memory) {
      memory = parseMemory(limits.memory);
    } else if (requests.memory) {
      memory = parseMemory(requests.memory);
    }
  }
  return { cpu, memory };
}

export function parseCPU(raw: any): number {
  let cpu = String(raw ?? '').trim();
  if (!cpu) {
    return 256;
  }
  const millicpu = cpu.endsWith('m');
  if (millicpu) {
    cpu = cpu.slice(0, -1);
  }
  let val = 0;
  let fracDigits = 0;
  for (const ch of cpu) {
    if (ch >= '0' && ch <= '9') {
      if (fracDigits > 0) {
        fracDigits++;
      }
      val = val * 10 + (ch.charCodeAt(0) - 48);
    } else if (ch === '.') {
      fracDigits = 1;
    }
  }
  if (millicpu) {
    val = Math.floor((val * 1024) / 1000);
  } else if (fracDigits > 0) {
    for (let i = 1; i < fracDigits; i++) {
      val = Math.floor(val / 10);
    }
    val = val * 1024;
  }
  if (val === 0) {
    return 256;
  }
  return val < 256 ? 256 : val;
}

export function parseMemory(raw: any): number {
  const mem = String(raw ?? '');
  let val = 0;
  for (const ch of mem) {
    if (ch >= '0' && ch <= '9') {
      val = val * 10 + (ch.charCodeAt(0) - 48);
    } else if (ch === 'G' || ch === 'g') {
      val = val * 1024;
      break;
    } else if (ch === 'M' || ch === 'm') {
      break;
    }
  }
  return val === 0 ? 512 : val;
}

export function sanitizeDatabaseName(value: string): string {
  const name = value.replace(/[^a-zA-Z0-9_]/g, '_').replace(/^_+|_+$/g, '').toLowerCase();
  if (!name) {
    return 'app';
  }
  return name.slice(0, 63);
}
