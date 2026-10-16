import * as cdk from 'aws-cdk-lib';
import { AssetCall } from '../_shared/helpers';
import { databaseAurora } from '../_shared/database';

/**
 * Aurora MySQL database template. Selected when the engine is not one of the
 * PostgreSQL engine names (see catalog match rules).
 */
export function databaseAuroraMysql(stack: cdk.Stack, name: string, call: AssetCall): void {
  databaseAurora(stack, name, call, false);
}
