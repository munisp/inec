import { useEffect, useState, useCallback } from 'react';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Progress } from '@/components/ui/progress';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Brain, Database, Cpu, RefreshCw, Activity, AlertTriangle, CheckCircle2, Layers } from 'lucide-react';

interface TierStats {
  bronze: number;
  silver: number;
  gold: number;
}

interface PipelineRun {
  id: string;
  tier: string;
  status: string;
  rows_in: number;
  rows_out: number;
  started_at: string;
  completed_at: string;
  error: string | null;
}

interface ModelVersion {
  name: string;
  version: string;
  status: string;
  metrics: Record<string, number>;
  registered_at: string;
}

interface DriftStatus {
  drift_detected: boolean;
  psi?: number;
  ks_statistic?: number;
  ks_p_value?: number;
  recommendation?: string;
}

interface TrainingStatus {
  production_model: ModelVersion | null;
  drift_status: DriftStatus;
  last_retrain: string | null;
  prediction_count: number;
  registry: { total_models: number; production_models: number };
}

const TIER_COLORS: Record<string, string> = {
  bronze: 'bg-orange-100 text-orange-800',
  silver: 'bg-gray-100 text-gray-800',
  gold: 'bg-yellow-100 text-yellow-800',
};

export default function MLDashboardPage() {
  const [lakehouseStatus, setLakehouseStatus] = useState<{ tiers: TierStats; recent_runs: PipelineRun[] } | null>(null);
  const [trainingStatus, setTrainingStatus] = useState<TrainingStatus | null>(null);
  const [modelRegistry, setModelRegistry] = useState<{ models: Record<string, ModelVersion>; production: Record<string, string> } | null>(null);
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState<string | null>(null);

  const loadData = useCallback(async () => {
    setLoading(true);
    try {
      const [lh, ts, mr] = await Promise.all([
        api.getLakehouseStatus().catch(() => ({ tiers: { bronze: 0, silver: 0, gold: 0 }, recent_runs: [] })),
        api.getTrainingStatus().catch(() => null),
        api.getModelRegistry().catch(() => ({ models: {}, production: {} })),
      ]);
      setLakehouseStatus(lh);
      setTrainingStatus(ts);
      setModelRegistry(mr);
    } catch { /* handled per-call */ }
    setLoading(false);
  }, []);

  useEffect(() => { loadData(); }, [loadData]);

  const handleAction = async (action: string) => {
    setActionLoading(action);
    try {
      if (action === 'ingest') await api.triggerLakehouseIngest(1);
      else if (action === 'pipeline') await api.triggerLakehousePipeline(1);
      else if (action === 'retrain') await api.triggerRetrain(false);
      else if (action === 'retrain-ray') await api.triggerRetrain(true);
      else if (action === 'batch-predict') await api.rayBatchPredict(1);
      else if (action === 'ray-train') await api.rayTrain();
      await loadData();
    } catch { /* handled */ }
    setActionLoading(null);
  };

  const tiers = lakehouseStatus?.tiers || { bronze: 0, silver: 0, gold: 0 };
  const totalRows = tiers.bronze + tiers.silver + tiers.gold;
  const recentRuns = lakehouseStatus?.recent_runs || [];
  const models = modelRegistry?.models || {};
  const productionModels = modelRegistry?.production || {};

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold flex items-center gap-2">
            <Brain className="h-6 w-6" /> ML/AI Infrastructure Dashboard
          </h1>
          <p className="text-muted-foreground">
            Lakehouse Pipeline, Model Registry, Continuous Training, Ray Compute
          </p>
        </div>
        <Button onClick={loadData} variant="outline" disabled={loading}>
          <RefreshCw className={`h-4 w-4 mr-2 ${loading ? 'animate-spin' : ''}`} />
          Refresh
        </Button>
      </div>

      {/* Summary Cards */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
        <Card>
          <CardHeader className="pb-2"><CardTitle className="text-sm font-medium flex items-center gap-1"><Database className="h-4 w-4" /> Lakehouse</CardTitle></CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{totalRows.toLocaleString()}</div>
            <p className="text-xs text-muted-foreground">Total rows across tiers</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2"><CardTitle className="text-sm font-medium flex items-center gap-1"><Layers className="h-4 w-4" /> Models</CardTitle></CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{Object.keys(models).length}</div>
            <p className="text-xs text-muted-foreground">{Object.keys(productionModels).length} in production</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2"><CardTitle className="text-sm font-medium flex items-center gap-1"><Activity className="h-4 w-4" /> Drift</CardTitle></CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {trainingStatus?.drift_status?.drift_detected ? (
                <span className="text-red-500">Detected</span>
              ) : (
                <span className="text-green-500">Stable</span>
              )}
            </div>
            <p className="text-xs text-muted-foreground">
              PSI: {trainingStatus?.drift_status?.psi?.toFixed(4) || 'N/A'}
            </p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2"><CardTitle className="text-sm font-medium flex items-center gap-1"><Cpu className="h-4 w-4" /> Ray</CardTitle></CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">Ready</div>
            <p className="text-xs text-muted-foreground">Distributed compute engine</p>
          </CardContent>
        </Card>
      </div>

      <Tabs defaultValue="lakehouse">
        <TabsList>
          <TabsTrigger value="lakehouse">Lakehouse</TabsTrigger>
          <TabsTrigger value="models">Model Registry</TabsTrigger>
          <TabsTrigger value="training">Continuous Training</TabsTrigger>
          <TabsTrigger value="ray">Ray Compute</TabsTrigger>
        </TabsList>

        {/* Lakehouse Tab */}
        <TabsContent value="lakehouse" className="space-y-4">
          <div className="grid grid-cols-3 gap-4">
            {(['bronze', 'silver', 'gold'] as const).map(tier => (
              <Card key={tier}>
                <CardHeader className="pb-2">
                  <CardTitle className="text-sm font-medium capitalize flex items-center gap-2">
                    <Badge className={TIER_COLORS[tier]}>{tier}</Badge>
                    {tier === 'bronze' && 'Raw Data'}
                    {tier === 'silver' && 'Cleaned + Enriched'}
                    {tier === 'gold' && 'ML-Ready Features'}
                  </CardTitle>
                </CardHeader>
                <CardContent>
                  <div className="text-3xl font-bold">{tiers[tier].toLocaleString()}</div>
                  <Progress value={totalRows > 0 ? (tiers[tier] / totalRows) * 100 : 0} className="mt-2" />
                </CardContent>
              </Card>
            ))}
          </div>

          <div className="flex gap-2">
            <Button onClick={() => handleAction('ingest')} disabled={actionLoading === 'ingest'}>
              {actionLoading === 'ingest' ? <RefreshCw className="h-4 w-4 mr-2 animate-spin" /> : <Database className="h-4 w-4 mr-2" />}
              Ingest Bronze
            </Button>
            <Button onClick={() => handleAction('pipeline')} variant="outline" disabled={actionLoading === 'pipeline'}>
              {actionLoading === 'pipeline' ? <RefreshCw className="h-4 w-4 mr-2 animate-spin" /> : <Layers className="h-4 w-4 mr-2" />}
              Run Full Pipeline
            </Button>
          </div>

          <Card>
            <CardHeader><CardTitle>Recent Pipeline Runs</CardTitle></CardHeader>
            <CardContent>
              <div className="space-y-2">
                {recentRuns.length === 0 && <p className="text-muted-foreground text-sm">No pipeline runs yet</p>}
                {recentRuns.map(run => (
                  <div key={run.id} className="flex items-center justify-between p-2 border rounded">
                    <div className="flex items-center gap-2">
                      <Badge className={TIER_COLORS[run.tier] || 'bg-gray-100'}>{run.tier}</Badge>
                      <span className="text-sm font-mono">{run.id}</span>
                    </div>
                    <div className="flex items-center gap-4 text-sm">
                      <span>{run.rows_in} → {run.rows_out} rows</span>
                      {run.status === 'completed' ? (
                        <Badge variant="outline" className="text-green-600"><CheckCircle2 className="h-3 w-3 mr-1" /> Done</Badge>
                      ) : run.status === 'failed' ? (
                        <Badge variant="destructive"><AlertTriangle className="h-3 w-3 mr-1" /> Failed</Badge>
                      ) : (
                        <Badge variant="secondary">Running</Badge>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        {/* Model Registry Tab */}
        <TabsContent value="models" className="space-y-4">
          <Card>
            <CardHeader><CardTitle>Registered Models</CardTitle></CardHeader>
            <CardContent>
              {Object.keys(models).length === 0 && <p className="text-muted-foreground text-sm">No models registered yet. Train models to populate the registry.</p>}
              <div className="space-y-3">
                {Object.entries(models).map(([id, model]) => (
                  <div key={id} className="p-3 border rounded flex items-center justify-between">
                    <div>
                      <div className="font-medium">{model.name} <span className="text-xs text-muted-foreground">v{model.version}</span></div>
                      <div className="text-xs text-muted-foreground">{model.registered_at}</div>
                      {model.metrics && (
                        <div className="flex gap-2 mt-1">
                          {Object.entries(model.metrics).map(([key, val]) => (
                            <Badge key={key} variant="outline" className="text-xs">
                              {key}: {typeof val === 'number' ? val.toFixed(4) : String(val)}
                            </Badge>
                          ))}
                        </div>
                      )}
                    </div>
                    <Badge variant={model.status === 'production' ? 'default' : 'secondary'}>
                      {model.status}
                    </Badge>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader><CardTitle>Production Models</CardTitle></CardHeader>
            <CardContent>
              <div className="space-y-2">
                {Object.entries(productionModels).map(([name, modelId]) => (
                  <div key={name} className="flex items-center justify-between p-2 border rounded">
                    <span className="font-medium">{name}</span>
                    <Badge>{String(modelId)}</Badge>
                  </div>
                ))}
                {Object.keys(productionModels).length === 0 && (
                  <p className="text-muted-foreground text-sm">No production models yet</p>
                )}
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        {/* Continuous Training Tab */}
        <TabsContent value="training" className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <Card>
              <CardHeader><CardTitle className="text-sm">Drift Detection</CardTitle></CardHeader>
              <CardContent>
                {trainingStatus?.drift_status ? (
                  <div className="space-y-2">
                    <div className="flex items-center gap-2">
                      {trainingStatus.drift_status.drift_detected ? (
                        <AlertTriangle className="h-5 w-5 text-red-500" />
                      ) : (
                        <CheckCircle2 className="h-5 w-5 text-green-500" />
                      )}
                      <span className="font-medium">
                        {trainingStatus.drift_status.drift_detected ? 'Drift Detected — Retrain Recommended' : 'Model Performance Stable'}
                      </span>
                    </div>
                    {trainingStatus.drift_status.psi !== undefined && (
                      <div className="text-sm">PSI: {trainingStatus.drift_status.psi.toFixed(4)} (threshold: 0.2)</div>
                    )}
                    {trainingStatus.drift_status.ks_p_value !== undefined && (
                      <div className="text-sm">KS p-value: {trainingStatus.drift_status.ks_p_value.toFixed(4)}</div>
                    )}
                  </div>
                ) : (
                  <p className="text-muted-foreground text-sm">Drift detection not yet initialized</p>
                )}
              </CardContent>
            </Card>
            <Card>
              <CardHeader><CardTitle className="text-sm">Training Pipeline</CardTitle></CardHeader>
              <CardContent>
                <div className="space-y-2 text-sm">
                  <div>Last retrain: {trainingStatus?.last_retrain || 'Never'}</div>
                  <div>Predictions tracked: {trainingStatus?.prediction_count || 0}</div>
                  <div>Total model versions: {trainingStatus?.registry?.total_models || 0}</div>
                </div>
              </CardContent>
            </Card>
          </div>

          <div className="flex gap-2">
            <Button onClick={() => handleAction('retrain')} disabled={actionLoading === 'retrain'}>
              {actionLoading === 'retrain' ? <RefreshCw className="h-4 w-4 mr-2 animate-spin" /> : <Brain className="h-4 w-4 mr-2" />}
              Retrain (Sequential)
            </Button>
            <Button onClick={() => handleAction('retrain-ray')} variant="outline" disabled={actionLoading === 'retrain-ray'}>
              {actionLoading === 'retrain-ray' ? <RefreshCw className="h-4 w-4 mr-2 animate-spin" /> : <Cpu className="h-4 w-4 mr-2" />}
              Retrain (Ray Distributed)
            </Button>
          </div>
        </TabsContent>

        {/* Ray Compute Tab */}
        <TabsContent value="ray" className="space-y-4">
          <Card>
            <CardHeader><CardTitle>Ray Distributed Compute Engine</CardTitle></CardHeader>
            <CardContent className="space-y-4">
              <div className="grid grid-cols-3 gap-4">
                <div className="p-3 border rounded text-center">
                  <div className="text-lg font-bold">Batch Predict</div>
                  <p className="text-xs text-muted-foreground">Score all PUs in parallel using XGBoost</p>
                  <Button className="mt-2" size="sm" onClick={() => handleAction('batch-predict')} disabled={actionLoading === 'batch-predict'}>
                    {actionLoading === 'batch-predict' ? <RefreshCw className="h-3 w-3 mr-1 animate-spin" /> : null}
                    Run
                  </Button>
                </div>
                <div className="p-3 border rounded text-center">
                  <div className="text-lg font-bold">Distributed Training</div>
                  <p className="text-xs text-muted-foreground">Train all models in parallel via Ray</p>
                  <Button className="mt-2" size="sm" onClick={() => handleAction('ray-train')} disabled={actionLoading === 'ray-train'}>
                    {actionLoading === 'ray-train' ? <RefreshCw className="h-3 w-3 mr-1 animate-spin" /> : null}
                    Train
                  </Button>
                </div>
                <div className="p-3 border rounded text-center">
                  <div className="text-lg font-bold">Feature Engineering</div>
                  <p className="text-xs text-muted-foreground">Compute ML features in parallel</p>
                  <Button className="mt-2" size="sm" onClick={() => handleAction('pipeline')} disabled={actionLoading === 'pipeline'}>
                    {actionLoading === 'pipeline' ? <RefreshCw className="h-3 w-3 mr-1 animate-spin" /> : null}
                    Compute
                  </Button>
                </div>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader><CardTitle>Architecture</CardTitle></CardHeader>
            <CardContent>
              <div className="text-sm space-y-1 font-mono bg-muted p-4 rounded">
                <div>┌─────────────────────────────────────────┐</div>
                <div>│  Go Backend (Port 8088)                 │</div>
                <div>│  ├── /ai/anomalies → XGBoost inference  │</div>
                <div>│  ├── /ai/gnn/score → GAT inference      │</div>
                <div>│  ├── /ai/lakehouse/* → Lakehouse ETL    │</div>
                <div>│  ├── /ai/ray/* → Distributed compute    │</div>
                <div>│  └── /ai/training/* → Continuous train  │</div>
                <div>├─────────────────────────────────────────┤</div>
                <div>│  Python ML Server (Port 8090)           │</div>
                <div>│  ├── XGBoost (ROC-AUC: 1.0000)          │</div>
                <div>│  ├── GAT GNN (F1: 0.9988)               │</div>
                <div>│  ├── CDCN Liveness (6.1M params)         │</div>
                <div>│  └── PaddleOCR (EC8A extraction)         │</div>
                <div>├─────────────────────────────────────────┤</div>
                <div>│  Lakehouse (DuckDB)                     │</div>
                <div>│  ├── Bronze: Raw ingestion (Parquet)    │</div>
                <div>│  ├── Silver: Cleaned + enriched         │</div>
                <div>│  └── Gold: ML-ready feature matrices    │</div>
                <div>├─────────────────────────────────────────┤</div>
                <div>│  Ray Cluster                            │</div>
                <div>│  ├── Distributed training (parallel)    │</div>
                <div>│  ├── Batch inference (1000/batch)       │</div>
                <div>│  └── Feature engineering (parallel)     │</div>
                <div>└─────────────────────────────────────────┘</div>
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}
