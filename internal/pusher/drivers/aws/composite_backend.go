package awsdriver

import "context"

type CompositeBackend struct {
	CDK *CLIBackend
	SDK *AWSBackend
}

func NewCompositeBackend(cdk *CLIBackend, sdk *AWSBackend) *CompositeBackend {
	return &CompositeBackend{CDK: cdk, SDK: sdk}
}

func (b *CompositeBackend) Synthesize(ctx context.Context, req StackRequest) error {
	return b.CDK.Synthesize(ctx, req)
}
func (b *CompositeBackend) CheckEnvironment(ctx context.Context, req StackRequest) error {
	return b.CDK.CheckEnvironment(ctx, req)
}
func (b *CompositeBackend) GetStack(ctx context.Context, req StackRequest) (StackState, bool, error) {
	return b.CDK.GetStack(ctx, req)
}
func (b *CompositeBackend) DetectDrift(ctx context.Context, req StackRequest) (StackDriftStatus, error) {
	return b.CDK.DetectDrift(ctx, req)
}
func (b *CompositeBackend) DeployStack(ctx context.Context, req StackRequest) (StackState, error) {
	return b.CDK.DeployStack(ctx, req)
}
func (b *CompositeBackend) DeleteStack(ctx context.Context, req StackRequest) error {
	return b.CDK.DeleteStack(ctx, req)
}

func (b *CompositeBackend) UpsertFileSystem(ctx context.Context, fs FileSystem) (string, error) {
	return b.SDK.UpsertFileSystem(ctx, fs)
}
func (b *CompositeBackend) GetFileSystem(ctx context.Context, fsID string) (FileSystem, bool, error) {
	return b.SDK.GetFileSystem(ctx, fsID)
}
func (b *CompositeBackend) DeleteFileSystem(ctx context.Context, fsID string) error {
	return b.SDK.DeleteFileSystem(ctx, fsID)
}
func (b *CompositeBackend) UpsertParameter(ctx context.Context, param Parameter) error {
	return b.SDK.UpsertParameter(ctx, param)
}
func (b *CompositeBackend) GetParameter(ctx context.Context, name string) (Parameter, bool, error) {
	return b.SDK.GetParameter(ctx, name)
}
func (b *CompositeBackend) DeleteParameter(ctx context.Context, name string) error {
	return b.SDK.DeleteParameter(ctx, name)
}
func (b *CompositeBackend) UpsertSecret(ctx context.Context, secret Secret) (string, error) {
	return b.SDK.UpsertSecret(ctx, secret)
}
func (b *CompositeBackend) GetSecret(ctx context.Context, secretID string) (Secret, bool, error) {
	return b.SDK.GetSecret(ctx, secretID)
}
func (b *CompositeBackend) DeleteSecret(ctx context.Context, secretID string) error {
	return b.SDK.DeleteSecret(ctx, secretID)
}
func (b *CompositeBackend) UpsertService(ctx context.Context, svc ECSService) error {
	return b.SDK.UpsertService(ctx, svc)
}
func (b *CompositeBackend) GetService(ctx context.Context, cluster, name string) (ECSService, bool, error) {
	return b.SDK.GetService(ctx, cluster, name)
}
func (b *CompositeBackend) DeleteService(ctx context.Context, cluster, name string) error {
	return b.SDK.DeleteService(ctx, cluster, name)
}
func (b *CompositeBackend) UpsertLoadBalancer(ctx context.Context, lb LoadBalancer) (string, error) {
	return b.SDK.UpsertLoadBalancer(ctx, lb)
}
func (b *CompositeBackend) GetLoadBalancer(ctx context.Context, name string) (LoadBalancer, bool, error) {
	return b.SDK.GetLoadBalancer(ctx, name)
}
func (b *CompositeBackend) DeleteLoadBalancer(ctx context.Context, arn string) error {
	return b.SDK.DeleteLoadBalancer(ctx, arn)
}
func (b *CompositeBackend) UpsertTargetGroup(ctx context.Context, tg TargetGroup) (string, error) {
	return b.SDK.UpsertTargetGroup(ctx, tg)
}
func (b *CompositeBackend) GetTargetGroup(ctx context.Context, name string) (TargetGroup, bool, error) {
	return b.SDK.GetTargetGroup(ctx, name)
}
func (b *CompositeBackend) DeleteTargetGroup(ctx context.Context, arn string) error {
	return b.SDK.DeleteTargetGroup(ctx, arn)
}
func (b *CompositeBackend) UpsertListener(ctx context.Context, listener Listener) (string, error) {
	return b.SDK.UpsertListener(ctx, listener)
}
func (b *CompositeBackend) GetListener(ctx context.Context, lbARN string, port int) (Listener, bool, error) {
	return b.SDK.GetListener(ctx, lbARN, port)
}
func (b *CompositeBackend) DeleteListener(ctx context.Context, arn string) error {
	return b.SDK.DeleteListener(ctx, arn)
}
func (b *CompositeBackend) UpsertBucket(ctx context.Context, bucket BucketSpec) error {
	return b.SDK.UpsertBucket(ctx, bucket)
}
func (b *CompositeBackend) GetBucket(ctx context.Context, name string) (BucketSpec, bool, error) {
	return b.SDK.GetBucket(ctx, name)
}
func (b *CompositeBackend) DeleteBucket(ctx context.Context, name string) error {
	return b.SDK.DeleteBucket(ctx, name)
}
func (b *CompositeBackend) UpsertLogGroup(ctx context.Context, group LogGroup) error {
	return b.SDK.UpsertLogGroup(ctx, group)
}
func (b *CompositeBackend) GetLogGroup(ctx context.Context, name string) (LogGroup, bool, error) {
	return b.SDK.GetLogGroup(ctx, name)
}
