import * as cdk from 'aws-cdk-lib';
import * as ec2 from 'aws-cdk-lib/aws-ec2';
import * as rds from 'aws-cdk-lib/aws-rds';
import { AssetCall, constructId, exportOutput, sanitizeDatabaseName } from './helpers';
import { NetworkHandle } from './handles';

/**
 * Shared Aurora implementation used by the postgres/mysql database template
 * variants. Kept in `_shared` so both variants stay in lockstep.
 */
export function databaseAurora(stack: cdk.Stack, name: string, call: AssetCall, postgres: boolean): void {
  const props = call.properties;
  const generated = call.generated;
  const id = constructId(name);
  const network = call.refs.network as NetworkHandle | undefined;
  if (!network) {
    throw new Error(`database ${name}: refs.network is required`);
  }
  const engine = postgres
    ? rds.DatabaseClusterEngine.auroraPostgres({ version: rds.AuroraPostgresEngineVersion.VER_15_4 })
    : rds.DatabaseClusterEngine.auroraMysql({ version: rds.AuroraMysqlEngineVersion.VER_3_06_0 });
  const port = Number(props.port ?? (postgres ? 5432 : 3306));
  const securityGroup = new ec2.SecurityGroup(stack, `DbSg${id}`, {
    vpc: network.vpc,
    allowAllOutbound: true,
    description: `Guardian database ${name}`,
  });
  const database = sanitizeDatabaseName(String(props.database || name));
  const user = String(props.user || 'guardian');
  const writer = rds.ClusterInstance.serverlessV2('writer');
  const cluster = new rds.DatabaseCluster(stack, `Db${id}`, {
    engine,
    clusterIdentifier: generated.clusterName,
    credentials: rds.Credentials.fromGeneratedSecret(user),
    defaultDatabaseName: database,
    writer,
    serverlessV2MinCapacity: 0.5,
    serverlessV2MaxCapacity: 2,
    port,
    vpc: network.vpc,
    vpcSubnets: { subnetType: ec2.SubnetType.PUBLIC },
    securityGroups: [securityGroup],
    storageEncrypted: true,
    removalPolicy: cdk.RemovalPolicy.DESTROY,
  });
  exportOutput(stack, name, 'id', cluster.clusterIdentifier);
  exportOutput(stack, name, 'engine', postgres ? 'aurora-postgresql' : 'aurora-mysql');
  exportOutput(stack, name, 'database', database);
  exportOutput(stack, name, 'user', user);
  exportOutput(stack, name, 'port', String(port));
  exportOutput(stack, name, 'arn', cluster.clusterArn);
  exportOutput(stack, name, 'endpoint', cluster.clusterEndpoint.hostname);
  exportOutput(stack, name, 'readerEndpoint', cluster.clusterReadEndpoint.hostname);
  exportOutput(stack, name, 'url', `${postgres ? 'postgresql' : 'mysql'}://${user}@${cluster.clusterEndpoint.hostname}:${port}/${database}`);
  if (cluster.secret) {
    exportOutput(stack, name, 'secretArn', cluster.secret.secretArn);
  }
}
