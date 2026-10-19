import * as cdk from 'aws-cdk-lib';
import * as ec2 from 'aws-cdk-lib/aws-ec2';
import * as ecs from 'aws-cdk-lib/aws-ecs';
import * as iam from 'aws-cdk-lib/aws-iam';
import * as logs from 'aws-cdk-lib/aws-logs';
import {
  AssetCall,
  boolValue,
  buildEnvironment,
  buildHealthCheck,
  buildPortMappings,
  buildSecrets,
  constructId,
  exportOutput,
  firstPort,
  objectList,
  resolveResources,
  stringList,
} from '../_shared/helpers';
import { ComputeHandle, NetworkHandle, VolumeHandle } from '../_shared/handles';

/**
 * Compute asset template: Fargate service on the intent network, optionally
 * mounting Volume handles (`refs.volumes`) and sharing an ECS cluster with
 * sibling computes (`refs.cluster`).
 */
export function computeFargate(stack: cdk.Stack, name: string, call: AssetCall): ComputeHandle {
  const props = call.properties;
  const generated = call.generated;
  const id = constructId(name);
  if (boolValue(props.privileged) || boolValue(props.observeExisting)) {
    throw new Error(`compute asset ${name}: privileged/observeExisting is not supported by generated stacks`);
  }
  const network = call.refs.network as NetworkHandle | undefined;
  if (!network) {
    throw new Error(`compute asset ${name}: refs.network is required`);
  }
  const volumes = (call.refs.volumes || {}) as Record<string, VolumeHandle>;

  const clusterName = String(generated.cluster || '');
  let cluster = call.refs.cluster as ecs.Cluster | undefined;
  if (!cluster) {
    cluster = new ecs.Cluster(stack, `Cluster${constructId(clusterName)}`, {
      vpc: network.vpc,
      clusterName,
    });
  }

  const securityGroup = new ec2.SecurityGroup(stack, `Sg${id}`, {
    vpc: network.vpc,
    allowAllOutbound: true,
    description: `Guardian compute ${name}`,
  });
  securityGroup.addIngressRule(securityGroup, ec2.Port.allTraffic(), 'self');

  const logGroup = new logs.LogGroup(stack, `Logs${id}`, {
    logGroupName: generated.logGroup,
    retention: logs.RetentionDays.ONE_MONTH,
    removalPolicy: cdk.RemovalPolicy.DESTROY,
  });

  const resources = resolveResources(props.resources);
  const taskDefinition = new ecs.FargateTaskDefinition(stack, `Task${id}`, {
    family: generated.taskFamily,
    cpu: resources.cpu,
    memoryLimitMiB: resources.memory,
  });

  // ContainerImage.fromRegistry does not infer ECR pull permissions, so grant
  // them explicitly to the task execution role.
  const executionRole = taskDefinition.obtainExecutionRole();
  executionRole.addToPrincipalPolicy(new iam.PolicyStatement({
    actions: ['ecr:GetAuthorizationToken'],
    resources: ['*'],
  }));
  executionRole.addToPrincipalPolicy(new iam.PolicyStatement({
    actions: [
      'ecr:BatchCheckLayerAvailability',
      'ecr:BatchGetImage',
      'ecr:GetDownloadUrlForLayer',
    ],
    resources: ['*'],
  }));

  const containerName = name;
  const container = taskDefinition.addContainer(`Container${id}`, {
    containerName,
    image: ecs.ContainerImage.fromRegistry(String(props.image)),
    command: stringList(props.command),
    environment: buildEnvironment(props.env, call.partition, call.intent, name, call.account, call.region),
    secrets: buildSecrets(stack, name, generated.configParams),
    portMappings: buildPortMappings(props.ports),
    healthCheck: buildHealthCheck(props.healthCheck),
    logging: ecs.LogDrivers.awsLogs({ logGroup, streamPrefix: name }),
    essential: true,
  });

  const addedVolumes = new Set<string>();
  for (const mount of objectList(props.volumeMounts)) {
    const volumeName = String(mount.volume || '');
    const handle = volumes[volumeName];
    if (!handle) {
      throw new Error(`compute asset ${name}: volume ${volumeName} was not generated before it`);
    }
    const sourceVolume = `efs-${volumeName}`;
    if (!addedVolumes.has(sourceVolume)) {
      taskDefinition.addVolume({
        name: sourceVolume,
        efsVolumeConfiguration: {
          fileSystemId: handle.fileSystem.ref,
          transitEncryption: 'ENABLED',
        },
      });
      addedVolumes.add(sourceVolume);
    }
    container.addMountPoints({
      containerPath: String(mount.path || ''),
      sourceVolume,
      readOnly: boolValue(mount.readOnly),
    });
    taskDefinition.taskRole.addToPrincipalPolicy(new iam.PolicyStatement({
      actions: [
        'elasticfilesystem:ClientMount',
        'elasticfilesystem:ClientWrite',
        'elasticfilesystem:ClientRootAccess',
        'elasticfilesystem:DescribeMountTargets',
      ],
      resources: [handle.fileSystem.attrArn],
    }));
  }

  const service = new ecs.FargateService(stack, `Service${id}`, {
    cluster,
    taskDefinition,
    serviceName: generated.serviceName,
    desiredCount: Number(props.replicas ?? 1),
    assignPublicIp: true,
    securityGroups: [securityGroup],
    vpcSubnets: { subnetType: ec2.SubnetType.PUBLIC },
    circuitBreaker: { rollback: true },
  });

  exportOutput(stack, name, 'service', service.serviceName);
  exportOutput(stack, name, 'launchType', 'FARGATE');
  exportOutput(stack, name, 'cluster', cluster.clusterName);

  return {
    assetName: name,
    service,
    taskDefinition,
    container,
    securityGroup,
    cluster,
    containerName,
    port: firstPort(props.ports),
  };
}
