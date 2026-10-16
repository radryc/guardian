import * as cdk from 'aws-cdk-lib';
import { AssetCall } from '../_shared/helpers';
import { databaseAurora } from '../_shared/database';

/**
 * Aurora PostgreSQL database template (default engine variant).
 */
export function databaseAuroraPostgres(stack: cdk.Stack, name: string, call: AssetCall): void {
  databaseAurora(stack, name, call, true);
}
