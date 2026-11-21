package awsdriver

func envKey(account, region string) string {
	return account + "/" + region
}

func stackKey(account, region, stackName string) string {
	return envKey(account, region) + "/" + stackName
}

func cloneStackState(in StackState) StackState {
	return StackState{
		ID:      in.ID,
		Name:    in.Name,
		Status:  in.Status,
		Tags:    cloneStringMap(in.Tags),
		Outputs: cloneStringMap(in.Outputs),
	}
}

func cloneRequest(in StackRequest) StackRequest {
	return StackRequest{
		PartitionName: in.PartitionName,
		IntentName:    in.IntentName,
		AssetName:     in.AssetName,
		Target:        in.Target,
		Manifest:      in.Manifest,
		WorkspaceDir:  in.WorkspaceDir,
		AppCommand:    in.AppCommand,
		Context:       cloneStringMap(in.Context),
		Env:           cloneStringMap(in.Env),
		DesiredHash:   in.DesiredHash,
		Tags:          cloneStringMap(in.Tags),
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
