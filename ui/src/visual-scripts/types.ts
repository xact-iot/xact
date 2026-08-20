export interface Position { x: number; y: number }
export interface GraphNode { id: string; type: string; typeVersion: number; position: Position; config: Record<string, any> }
export interface GraphEdge { id: string; from: { nodeId: string; port: string }; to: { nodeId: string; port: string } }
export interface GraphDocument {
  schemaVersion: number;
  settings: { maxConcurrency: number; queueLimit: number; errorPolicy: string; traceLevel: string };
  nodes: GraphNode[];
  edges: GraphEdge[];
  annotations: any[];
}
export interface Diagnostic { severity: 'error' | 'warning'; code: string; nodeId?: string; edgeId?: string; field?: string; message: string }
export interface ValidationResult { valid: boolean; diagnostics: Diagnostic[]; graph?: GraphDocument; graphHash?: string }
export interface VisualScript {
  id: string; name: string; description: string; desiredState: 'stopped' | 'running' | 'paused';
  latestRevision: number; activeRevision?: number; createdAt: string; updatedAt: string;
  runtimeState?: string; outOfDate: boolean; hasBackup?: boolean; simulation?: boolean; activate?: boolean;
}
export interface VisualScriptRevision { scriptId: string; revision: number; schemaVersion: number; graph: GraphDocument; graphHash: string; validationStatus: string; diagnostics: Diagnostic[]; createdAt: string }
export interface PortDefinition { name: string; label: string; dataType: string }
export interface ParameterDefinition { name: string; label: string; type: 'string'|'number'|'boolean'|'select'|'json'|'tag-path'; description?: string; required?: boolean; options?: string[]; default?: any }
export interface NodeDefinition { type: string; typeVersion: number; name: string; description: string; category: string; icon: string; inputs: PortDefinition[]; outputs: PortDefinition[]; parameters: ParameterDefinition[]; available: boolean; unavailableReason?: string; outputNode?: boolean; simulationSafe?: boolean }
export interface RuntimeStatus { scriptId: string; desiredState: string; runtimeState: string; activeRevision?: number; latestRevision: number; queueDepth: number; sequence: number; lastTriggerAt?: string; lastCompletionAt?: string; errorSummary?: string }
export interface TraceEvent { sequence: number; timestamp: string; nodeId: string; nodeType: string; port?: string; status: string; value?: any; fields?: Record<string, any>; message?: string; formattedTimes?: Record<string, string> }
export interface VisualScriptRun { runId: string; scriptId: string; activeRevision: number; triggerNodeId: string; instanceKey: string; startedAt: string; completedAt?: string; status: string; durationMs: number; message?: string; nodesExecuted: number; trace?: TraceEvent[] }

export function emptyGraph(): GraphDocument {
  return { schemaVersion: 1, settings: { maxConcurrency: 1, queueLimit: 100, errorPolicy: 'stop-message', traceLevel: 'errors' }, nodes: [], edges: [], annotations: [] };
}
