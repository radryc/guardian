import * as cdk from 'aws-cdk-lib';
import * as ec2 from 'aws-cdk-lib/aws-ec2';
import * as ecs from 'aws-cdk-lib/aws-ecs';
import * as efs from 'aws-cdk-lib/aws-efs';

/**
 * Cross-template handle types. Kept in `_shared` so templates never import
 * types from each other (the composer only copies selected template
 * directories into the payload).
 */

export interface NetworkHandle {
  vpc: ec2.Vpc;
  vpcName: string;
}

export interface VolumeHandle {
  fileSystem: efs.CfnFileSystem;
}

export interface ComputeHandle {
  assetName: string;
  service: ecs.FargateService;
  taskDefinition: ecs.FargateTaskDefinition;
  container: ecs.ContainerDefinition;
  securityGroup: ec2.SecurityGroup;
  cluster: ecs.Cluster;
  containerName: string;
  port: number;
}

export interface ObjectStoreHandle {
  bucketName: string;
}

export interface CfnResourceHandle {
  resource: cdk.CfnResource;
}
