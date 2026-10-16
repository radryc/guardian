import * as cdk from 'aws-cdk-lib';
import * as ecs from 'aws-cdk-lib/aws-ecs';
import * as iam from 'aws-cdk-lib/aws-iam';
import * as logs from 'aws-cdk-lib/aws-logs';
import { AssetCall, constructId, exportOutput, stringList } from '../_shared/helpers';
import { ComputeHandle } from '../_shared/handles';

const OTEL_COLLECTOR_IMAGE = 'public.ecr.aws/aws-observability/aws-otel-collector:v0.50.0';

/**
 * Observability asset template: injects an OTEL collector sidecar into the
 * target compute task definitions and points their containers at it.
 */
export function observabilityOtelSidecar(stack: cdk.Stack, name: string, call: AssetCall): void {
  const props = call.properties;
  const generated = call.generated;
  const id = constructId(name);
  if (props.config) {
    throw new Error(`observability asset ${name}: Config-backed collector config is not supported by generated stacks yet`);
  }
  const targets = (call.refs.targets || []) as ComputeHandle[];
  const config = renderOtelConfig(name, props, generated, cdk.Stack.of(stack).region);
  for (const target of targets) {
    const logGroup = new logs.LogGroup(stack, `OtelLogs${id}${constructId(target.assetName)}`, {
      logGroupName: generated.logGroup,
      retention: logs.RetentionDays.ONE_MONTH,
      removalPolicy: cdk.RemovalPolicy.DESTROY,
    });
    target.taskDefinition.addContainer(`Otel${id}${constructId(target.assetName)}`, {
      containerName: 'otel-collector',
      image: ecs.ContainerImage.fromRegistry(OTEL_COLLECTOR_IMAGE),
      command: ['--config=env:AOT_CONFIG_CONTENT'],
      environment: {
        AOT_CONFIG_CONTENT: config,
        AWS_REGION: cdk.Stack.of(stack).region,
      },
      essential: false,
      logging: ecs.LogDrivers.awsLogs({ logGroup, streamPrefix: 'otel' }),
    });
    target.container.addEnvironment('OTEL_EXPORTER_OTLP_ENDPOINT', 'http://localhost:4317');
    target.container.addEnvironment('OTEL_EXPORTER_OTLP_PROTOCOL', 'grpc');
    target.taskDefinition.taskRole.addToPrincipalPolicy(new iam.PolicyStatement({
      actions: [
        'cloudwatch:PutMetricData',
        'logs:CreateLogStream',
        'logs:PutLogEvents',
        'xray:PutTraceSegments',
        'xray:PutTelemetryRecords',
      ],
      resources: ['*'],
    }));
  }
  exportOutput(stack, name, 'endpoint', 'http://localhost:4317');
  exportOutput(stack, name, 'namespace', generated.namespace);
  exportOutput(stack, name, 'logGroup', generated.logGroup);
}

function renderOtelConfig(asset: string, props: any, generated: any, region: string): string {
  const exporters: Record<string, any> = {
    awsemf: {
      namespace: generated.namespace,
      region,
      log_group_name: generated.logGroup,
      log_stream_name: asset,
      dimension_rollup_option: 'NoDimensionRollup',
      resource_to_telemetry_conversion: { enabled: true },
    },
  };
  const metricExporters = ['awsemf'];
  const endpoint = String(props.endpoint || '').trim();
  if (endpoint) {
    const protocol = String(props.protocol || '').toLowerCase();
    if (protocol === 'http' || protocol === 'http/protobuf' || endpoint.startsWith('http://') || endpoint.startsWith('https://')) {
      exporters.otlphttp = { endpoint };
      metricExporters.push('otlphttp');
    } else {
      exporters.otlp = { endpoint };
      metricExporters.push('otlp');
    }
  }
  const processors: Record<string, any> = { batch: {} };
  const pipelines: Record<string, any> = {
    metrics: { receivers: ['otlp'], processors: ['batch'], exporters: metricExporters },
  };
  const exporterNames = stringList(props.exporters).map((exporter) => exporter.toLowerCase());
  if (exporterNames.includes('xray') || exporterNames.includes('awsxray')) {
    exporters.awsxray = { region };
    processors['attributes/xray'] = {
      actions: [{ key: 'peer.service', action: 'delete' }],
    };
    pipelines.traces = {
      receivers: ['otlp'],
      processors: ['attributes/xray', 'batch'],
      exporters: ['awsxray'],
    };
  }
  return JSON.stringify({
    receivers: { otlp: { protocols: { grpc: { endpoint: '0.0.0.0:4317' }, http: { endpoint: '0.0.0.0:4318' } } } },
    processors,
    exporters,
    service: { pipelines },
  });
}
