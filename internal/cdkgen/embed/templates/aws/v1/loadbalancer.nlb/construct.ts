import * as cdk from 'aws-cdk-lib';
import * as elbv2 from 'aws-cdk-lib/aws-elasticloadbalancingv2';
import { AssetCall, constructId, exportOutput, objectList } from '../_shared/helpers';
import { ComputeHandle, NetworkHandle } from '../_shared/handles';

/**
 * Network load balancer template. Selected when any listener declares a TCP
 * protocol (see catalog match rules).
 */
export function loadBalancerNlb(stack: cdk.Stack, name: string, call: AssetCall): void {
  const props = call.properties;
  const generated = call.generated;
  const id = constructId(name);
  const network = call.refs.network as NetworkHandle | undefined;
  if (!network) {
    throw new Error(`load balancer ${name}: refs.network is required`);
  }
  const targets = (call.refs.targets || []) as ComputeHandle[];
  const listeners = objectList(props.listeners);
  const internetFacing = props.serviceType !== 'ClusterIP';
  const tgNames = Array.isArray(generated.tgNames) ? generated.tgNames : [];

  const lb = new elbv2.NetworkLoadBalancer(stack, `Lb${id}`, {
    vpc: network.vpc,
    internetFacing,
    loadBalancerName: generated.lbName,
  });
  listeners.forEach((listener, index) => {
    const backendPort = targets[0]?.port || Number(listener.port || 80);
    const targetGroup = new elbv2.NetworkTargetGroup(stack, `Tg${id}${index}`, {
      vpc: network.vpc,
      port: backendPort,
      protocol: elbv2.Protocol.TCP,
      targetType: elbv2.TargetType.IP,
      targetGroupName: tgNames[index],
    });
    for (const target of targets) {
      targetGroup.addTarget(target.service.loadBalancerTarget({
        containerName: target.containerName,
        containerPort: target.port,
      }));
    }
    lb.addListener(`L${id}${index}`, { port: Number(listener.port || 80) }).addTargetGroups(
      `Ltg${id}${index}`,
      targetGroup,
    );
    exportOutput(stack, name, `tg_${index}_arn`, targetGroup.targetGroupArn);
  });
  exportOutput(stack, name, 'lb_arn', lb.loadBalancerArn);
  exportOutput(stack, name, 'lb_name', lb.loadBalancerName);
  exportOutput(stack, name, 'dns_name', lb.loadBalancerDnsName);
}
