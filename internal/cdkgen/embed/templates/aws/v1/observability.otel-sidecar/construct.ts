import * as cdk from 'aws-cdk-lib';
import * as ec2 from 'aws-cdk-lib/aws-ec2';
import * as ecs from 'aws-cdk-lib/aws-ecs';
import * as iam from 'aws-cdk-lib/aws-iam';
import * as logs from 'aws-cdk-lib/aws-logs';
import * as servicediscovery from 'aws-cdk-lib/aws-servicediscovery';
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
  const mode = String(props.mode || '').toLowerCase();
  if (mode === 'gateway' || mode === 'auto' || mode === 'collector') {
    observabilityGateway(stack, name, call, targets);
    return;
  }
  const sidecarLogGroup = `${generated.logGroup}-sidecar`;
  const sidecarGenerated = { ...generated, logGroup: sidecarLogGroup } as any;
  const config = renderOtelConfig(name, props, sidecarGenerated, cdk.Stack.of(stack).region);
  for (const target of targets) {
    const logGroup = new logs.LogGroup(stack, `OtelLogs${id}${constructId(target.assetName)}`, {
      logGroupName: sidecarGenerated.logGroup,
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
  exportOutput(stack, name, 'namespace', sidecarGenerated.namespace);
  exportOutput(stack, name, 'logGroup', sidecarGenerated.logGroup);
}

function observabilityGateway(stack: cdk.Stack, name: string, call: AssetCall, targets: ComputeHandle[]): void {
  if (targets.length === 0) {
    throw new Error(`observability asset ${name}: gateway mode requires at least one target compute asset`);
  }
  const props = call.properties;
  const generated = call.generated;
  const id = constructId(name);
  const region = cdk.Stack.of(stack).region;
  const cluster = targets[0].cluster;
  const vpc = cluster.vpc;
  const securityGroup = new ec2.SecurityGroup(stack, `ObsGwSg${id}`, {
    vpc,
    allowAllOutbound: true,
    description: `Guardian observability gateway ${name}`,
  });
  securityGroup.addIngressRule(securityGroup, ec2.Port.allTraffic(), 'gateway self');
  for (const target of targets) {
    securityGroup.addIngressRule(target.securityGroup, ec2.Port.tcp(4317), 'otlp grpc');
    securityGroup.addIngressRule(target.securityGroup, ec2.Port.tcp(4318), 'otlp http');
  }
  const logGroup = new logs.LogGroup(stack, `ObsGwLogs${id}`, {
    logGroupName: generated.logGroup,
    retention: logs.RetentionDays.ONE_MONTH,
    removalPolicy: cdk.RemovalPolicy.DESTROY,
  });
  const taskDefinition = new ecs.FargateTaskDefinition(stack, `ObsGwTask${id}`, {
    cpu: 256,
    memoryLimitMiB: 512,
  });
  taskDefinition.taskRole.addToPrincipalPolicy(new iam.PolicyStatement({
    actions: [
      'cloudwatch:PutMetricData',
      'logs:CreateLogGroup',
      'logs:CreateLogStream',
      'logs:PutLogEvents',
      'xray:PutTraceSegments',
      'xray:PutTelemetryRecords',
    ],
    resources: ['*'],
  }));
  const config = renderOtelConfig(name, props, generated, region);
  const container = taskDefinition.addContainer(`ObsGwContainer${id}`, {
    containerName: 'otel-collector',
    image: ecs.ContainerImage.fromRegistry(OTEL_COLLECTOR_IMAGE),
    command: ['--config=env:AOT_CONFIG_CONTENT'],
    environment: {
      AOT_CONFIG_CONTENT: config,
      AWS_REGION: region,
    },
    logging: ecs.LogDrivers.awsLogs({ logGroup, streamPrefix: 'otel-gateway' }),
    essential: true,
  });
  container.addPortMappings(
    { containerPort: 4317, protocol: ecs.Protocol.TCP, name: 'otlp-grpc' },
    { containerPort: 4318, protocol: ecs.Protocol.TCP, name: 'otlp-http' },
  );
  let namespace = stack.node.tryFindChild('GuardianOtelNamespace') as servicediscovery.PrivateDnsNamespace | undefined;
  if (!namespace) {
    namespace = new servicediscovery.PrivateDnsNamespace(stack, 'GuardianOtelNamespace', {
      name: 'guardian.local',
      vpc,
    });
  }
  const dnsName = `otel-${dnsPart(call.partition)}-${dnsPart(call.intent)}`;
  const service = new ecs.FargateService(stack, `ObsGwService${id}`, {
    cluster,
    taskDefinition,
    serviceName: `obs-gateway-${id}`.slice(0, 255),
    desiredCount: 1,
    assignPublicIp: true,
    securityGroups: [securityGroup],
    vpcSubnets: { subnetType: ec2.SubnetType.PUBLIC },
    serviceConnectConfiguration: {
      namespace: namespace.namespaceName,
      services: [{ portMappingName: 'otlp-grpc', dnsName, port: 4317 }],
    },
  });
  // The ECS service resolves the namespace by name at create time; without an
  // explicit dependency CloudFormation can create both concurrently and the
  // service fails with "Failed to retrieve namespace".
  service.node.addDependency(namespace);
  for (const target of targets) {
    target.container.addEnvironment('OTEL_EXPORTER_OTLP_ENDPOINT', `http://${dnsName}:4317`);
    target.container.addEnvironment('OTEL_EXPORTER_OTLP_PROTOCOL', 'grpc');
  }
  exportOutput(stack, name, 'endpoint', `http://${dnsName}:4317`);
  exportOutput(stack, name, 'namespace', generated.namespace);
  exportOutput(stack, name, 'logGroup', generated.logGroup);
}

function dnsPart(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9-]+/g, '-').replace(/^-+|-+$/g, '') || 'service';
}

function renderOtelConfig(asset: string, props: any, generated: any, region: string): string {
  const mode = String(props.mode || '').toLowerCase();
  const cloudwatchEnabled = props.cloudwatch !== false;
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
  const extensions: Record<string, any> = {};
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
  if (cloudwatchEnabled) {
    // Native CloudWatch OTLP metric ingestion + PromQL. EMF is still sent for
    // classic CloudWatch metrics; this exporter feeds the native OTel store.
    exporters.otlphttp = {
      metrics_endpoint: `https://otlp.${region}.amazonaws.com/v1/metrics`,
      auth: { authenticator: 'sigv4auth' },
      compression: 'gzip',
    };
    metricExporters.push('otlphttp');
    extensions.sigv4auth = { region, service: 'monitoring' };
  }
  const processors: Record<string, any> = { batch: {} };
  const pipelines: Record<string, any> = {
    metrics: { receivers: ['otlp'], processors: ['batch'], exporters: metricExporters },
  };
  if (cloudwatchEnabled) {
    exporters.awscloudwatchlogs = {
      region,
      log_group_name: generated.logGroup,
      log_stream_name: asset,
    };
    pipelines.logs = { receivers: ['otlp'], processors: ['batch'], exporters: ['awscloudwatchlogs'] };
  }
  const exporterNames = stringList(props.exporters).map((exporter) => exporter.toLowerCase());
  const gatewayMode = mode === 'gateway' || mode === 'auto' || mode === 'collector';
  if (exporterNames.includes('xray') || exporterNames.includes('awsxray') || (gatewayMode && cloudwatchEnabled)) {
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
    extensions,
    service: { extensions: Object.keys(extensions), pipelines },
  });
}
