import * as cdk from 'aws-cdk-lib';
import * as ec2 from 'aws-cdk-lib/aws-ec2';
import { AssetCall, exportOutput, constructId } from '../_shared/helpers';
import { NetworkHandle } from '../_shared/handles';

/**
 * Network asset template: creates the VPC other templates attach to.
 * Declared once per stack; consumers receive the handle via `refs.network`.
 */
export function networkVpc(stack: cdk.Stack, name: string, call: AssetCall): NetworkHandle {
  const id = constructId(name);
  const vpcName = String(call.generated.vpcName || '');
  const vpc = new ec2.Vpc(stack, `Vpc${id}`, {
    vpcName,
    ipAddresses: ec2.IpAddresses.cidr(String(call.properties.cidr || '10.0.0.0/16')),
    maxAzs: 2,
    natGateways: 0,
    subnetConfiguration: [
      { name: 'public', subnetType: ec2.SubnetType.PUBLIC, cidrMask: 24 },
    ],
  });
  exportOutput(stack, name, 'vpcId', vpc.vpcId);
  exportOutput(stack, name, 'cidr', String(call.properties.cidr || '10.0.0.0/16'));
  exportOutput(stack, name, 'name', vpcName);
  return { vpc, vpcName };
}
