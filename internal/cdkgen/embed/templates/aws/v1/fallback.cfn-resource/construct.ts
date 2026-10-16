import * as cdk from 'aws-cdk-lib';
import { AssetCall, constructId, exportOutput } from '../_shared/helpers';
import { CfnResourceHandle } from '../_shared/handles';

/**
 * Fallback template for AWS resource types that have no curated Guardian
 * template yet (`AWSResource` assets). Wraps the raw CloudFormation resource
 * type with its properties so the resource can be deployed and later promoted
 * to a curated template.
 *
 * Outputs are pass-through strings supplied by the asset (`properties.outputs`)
 * because generic Ref/GetAtt resolution is not wired into cross-asset refs yet.
 */
export function cfnResource(stack: cdk.Stack, name: string, call: AssetCall): CfnResourceHandle {
  const cfnType = String(call.properties.cfnType || '').trim();
  if (!cfnType) {
    throw new Error(`AWSResource asset ${name}: cfnType is required`);
  }
  const id = constructId(name);
  const properties = (call.properties.properties || {}) as Record<string, any>;
  const resource = new cdk.CfnResource(stack, `Res${id}`, {
    type: cfnType,
    properties,
  });
  const outputs = call.properties.outputs;
  if (outputs && typeof outputs === 'object') {
    for (const [key, value] of Object.entries(outputs)) {
      exportOutput(stack, name, key, String(value ?? ''));
    }
  }
  return { resource };
}
