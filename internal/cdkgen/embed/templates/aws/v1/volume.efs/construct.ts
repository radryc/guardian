import * as cdk from 'aws-cdk-lib';
import * as ec2 from 'aws-cdk-lib/aws-ec2';
import * as efs from 'aws-cdk-lib/aws-efs';
import { AssetCall, constructId, exportOutput } from '../_shared/helpers';
import { NetworkHandle, VolumeHandle } from '../_shared/handles';

/**
 * Volume asset template: encrypted EFS filesystem plus mount targets in the
 * network's public subnets. Consumers receive the handle via `refs.volumes`.
 */
export function volumeEfs(stack: cdk.Stack, name: string, call: AssetCall): VolumeHandle {
  const id = constructId(name);
  const network = call.refs.network as NetworkHandle | undefined;
  if (!network) {
    throw new Error(`volume asset ${name}: refs.network is required`);
  }
  const fileSystem = new efs.CfnFileSystem(stack, `Efs${id}`, {
    encrypted: true,
    fileSystemTags: [
      { key: 'Name', value: String(call.generated.fileSystemName || '') },
      { key: 'guardian-asset', value: name },
    ],
  });
  const mountSg = new ec2.SecurityGroup(stack, `EfsSg${id}`, {
    vpc: network.vpc,
    allowAllOutbound: false,
    description: `Guardian EFS mount targets for ${name}`,
  });
  mountSg.addIngressRule(ec2.Peer.ipv4(network.vpc.vpcCidrBlock), ec2.Port.tcp(2049));
  network.vpc.publicSubnets.forEach((subnet, index) => {
    new efs.CfnMountTarget(stack, `EfsMt${id}${index}`, {
      fileSystemId: fileSystem.ref,
      subnetId: subnet.subnetId,
      securityGroups: [mountSg.securityGroupId],
    });
  });
  exportOutput(stack, name, 'efsId', fileSystem.ref);
  exportOutput(stack, name, 'type', 'efs');
  return { fileSystem };
}
