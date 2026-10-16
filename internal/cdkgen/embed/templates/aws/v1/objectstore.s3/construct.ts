import * as cdk from 'aws-cdk-lib';
import * as s3 from 'aws-cdk-lib/aws-s3';
import { AssetCall, boolValue, constructId, exportOutput } from '../_shared/helpers';
import { ObjectStoreHandle } from '../_shared/handles';

/**
 * ObjectStore asset template: an S3 bucket with Guardian-managed lifecycle.
 */
export function objectStoreS3(stack: cdk.Stack, name: string, call: AssetCall): ObjectStoreHandle {
  const id = constructId(name);
  const bucketName = String(call.generated.bucketName || '');
  new s3.Bucket(stack, `Bucket${id}`, {
    bucketName,
    versioned: boolValue(call.properties.versioning),
    encryption: s3.BucketEncryption.S3_MANAGED,
    enforceSSL: true,
    autoDeleteObjects: true,
    removalPolicy: cdk.RemovalPolicy.DESTROY,
  });
  const region = String(call.properties.region || cdk.Stack.of(stack).region);
  exportOutput(stack, name, 'bucket', bucketName);
  exportOutput(stack, name, 'region', region);
  exportOutput(stack, name, 'type', 's3');
  return { bucketName };
}
