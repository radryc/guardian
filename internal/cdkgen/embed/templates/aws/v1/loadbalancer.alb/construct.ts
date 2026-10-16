import * as cdk from 'aws-cdk-lib';
import * as ec2 from 'aws-cdk-lib/aws-ec2';
import * as elbv2 from 'aws-cdk-lib/aws-elasticloadbalancingv2';
import { AssetCall, constructId, exportOutput, objectList } from '../_shared/helpers';
import { ComputeHandle, NetworkHandle } from '../_shared/handles';

/**
 * Application load balancer template (default HTTP variant). Selected for
 * listeners without a TCP protocol; the composer picks the NLB template
 * otherwise.
 */
export function loadBalancerAlb(stack: cdk.Stack, name: string, call: AssetCall): void {
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

  const lb = new elbv2.ApplicationLoadBalancer(stack, `Lb${id}`, {
    vpc: network.vpc,
    internetFacing,
    loadBalancerName: generated.lbName,
  });
  listeners.forEach((listener, index) => {
    const listenerPort = Number(listener.port || 80);
    const backendPort = targets[0]?.port || listenerPort;
    const targetGroup = new elbv2.ApplicationTargetGroup(stack, `Tg${id}${index}`, {
      vpc: network.vpc,
      port: backendPort,
      protocol: elbv2.ApplicationProtocol.HTTP,
      targetType: elbv2.TargetType.IP,
      targetGroupName: tgNames[index],
      deregistrationDelay: cdk.Duration.seconds(30),
    });
    for (const target of targets) {
      targetGroup.addTarget(target.service.loadBalancerTarget({
        containerName: target.containerName,
        containerPort: target.port,
      }));
      target.securityGroup.addIngressRule(
        lb.connections.securityGroups[0],
        ec2.Port.tcp(target.port),
        `load balancer ${name}`,
      );
    }
    lb.addListener(`L${id}${index}`, { port: listenerPort }).addTargetGroups(`Ltg${id}${index}`, {
      targetGroups: [targetGroup],
    });
    exportOutput(stack, name, `tg_${index}_arn`, targetGroup.targetGroupArn);
  });
  exportOutput(stack, name, 'lb_arn', lb.loadBalancerArn);
  exportOutput(stack, name, 'lb_name', lb.loadBalancerName);
  exportOutput(stack, name, 'dns_name', lb.loadBalancerDnsName);
}
