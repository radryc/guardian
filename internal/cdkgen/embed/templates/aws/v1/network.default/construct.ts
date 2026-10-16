import * as cdk from 'aws-cdk-lib';
import * as ec2 from 'aws-cdk-lib/aws-ec2';
import { NetworkHandle } from '../_shared/handles';

/**
 * Implicit default network template. Instantiated by the Guardian composer
 * when an intent has no Network asset but another template needs a VPC.
 * Uses the same construct ID (`DefaultVpc`) as the historical monolithic
 * construct library so CloudFormation logical IDs stay stable.
 */
export function networkDefault(stack: cdk.Stack): NetworkHandle {
  const vpc = new ec2.Vpc(stack, 'DefaultVpc', {
    maxAzs: 2,
    natGateways: 0,
    subnetConfiguration: [
      { name: 'public', subnetType: ec2.SubnetType.PUBLIC, cidrMask: 24 },
    ],
  });
  return { vpc, vpcName: '' };
}
