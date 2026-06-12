export interface AppState {
  activePanel: string;
  selectedPartition: string;
  overview: any;
  detail: any;
  expandedAssetKey: string;
  expandedRolloutKeys: Record<string, boolean>;
  history: any;
  historyLoading: boolean;
  historyError: string;
  rollouts: any;
  rolloutsLoading: boolean;
  rolloutsError: string;
  catalog: any;
  topology: {
    zoom: number;
    nodePositions: Record<string, { x: number; y: number }>;
    selectedNodeId: string;
  };
  activityDrawer: {
    intentName: string;
    data: any;
    loading: boolean;
    error: string;
  };
  historyOptions: {
    limit: number;
    since: string;
    until: string;
  };
  refreshTimer: number | undefined;
  fastRefreshUntil: number;
  diagnosticDetails: Record<string, string>;
}

export function createState(): AppState {
  return {
    activePanel: "overviewPanel",
    selectedPartition: "",
    overview: null,
    detail: null,
    expandedAssetKey: "",
    expandedRolloutKeys: {},
    history: null,
    historyLoading: false,
    historyError: "",
    rollouts: null,
    rolloutsLoading: false,
    rolloutsError: "",
    catalog: null,
    topology: {
      zoom: 1,
      nodePositions: {},
      selectedNodeId: "",
    },
    activityDrawer: {
      intentName: "",
      data: null,
      loading: false,
      error: "",
    },
    historyOptions: {
      limit: 10,
      since: "",
      until: "",
    },
    refreshTimer: undefined,
    fastRefreshUntil: 0,
    diagnosticDetails: {},
  };
}
