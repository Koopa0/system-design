# 模型訓練平台 (MLOps Platform)

## 系統概述

模型訓練平台是一個完整的 MLOps 系統，涵蓋機器學習模型從開發到生產的全生命週期，解決資料科學團隊在實驗追蹤、版本管理、分散式訓練、模型部署等方面的痛點。

### 核心能力

1. **實驗追蹤與管理** - 記錄每次訓練的參數、指標、產出物
2. **資料版本控制** - 追蹤資料集變更，確保可重現性
3. **分散式訓練** - 多 GPU/多節點訓練，加速模型開發
4. **超參數優化** - 自動搜尋最佳模型配置
5. **模型版本管理** - 追蹤模型演進，支援回滾
6. **自動化部署** - CI/CD pipeline，安全上線新模型
7. **持續監控** - 檢測模型降級，自動觸發重訓練

### 應用場景

- **推薦系統**：個性化推薦模型訓練與更新
- **預測模型**：需求預測、價格預測、風險評估
- **NLP 應用**：文本分類、情感分析、命名實體識別
- **電腦視覺**：圖像分類、物體偵測、影像分割
- **時間序列**：異常檢測、趨勢預測

## 功能需求

### 1. 核心功能

#### 1.1 實驗管理
- 自動記錄訓練參數、指標、產出物
- 實驗比較與視覺化
- 分散式實驗追蹤（多團隊協作）
- 實驗重現：一鍵重現歷史實驗

#### 1.2 資料管理
- 大型資料集版本控制（TB 級）
- 資料血緣追蹤
- 資料處理 Pipeline 管理
- 特徵庫（Feature Store）

#### 1.3 訓練管理
- 訓練任務排程
- GPU 資源分配
- 分散式訓練支援（Data/Model/Pipeline Parallelism）
- Checkpoint 與斷點續訓
- 提前停止（Early Stopping）

#### 1.4 模型管理
- 模型版本控制
- 模型註冊表（Registry）
- 模型生命週期管理（Staging → Production → Archived）
- 模型元資料追蹤

### 2. 非功能需求

| 需求 | 指標 | 說明 |
|------|------|------|
| **訓練吞吐量** | 100+ 並發任務 | 支援多團隊同時訓練 |
| **資源利用率** | > 80% GPU 使用率 | 高效調度，減少閒置 |
| **實驗追蹤延遲** | < 100ms | 記錄指標不影響訓練速度 |
| **可擴展性** | 1000+ GPU | 水平擴展至大規模叢集 |
| **可用性** | 99.9% | 訓練服務高可用 |
| **資料吞吐** | 10 GB/s | 高速資料載入，避免 GPU 閒置 |

## 技術架構

### 系統架構圖

```
┌─────────────────────────────────────────────────────────────────┐
│                       Developer Interface                        │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐       │
│  │ Jupyter  │  │  VSCode  │  │   CLI    │  │  Web UI  │       │
│  │ Notebook │  │   IDE    │  │          │  │ (MLflow) │       │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘       │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                         API Gateway                              │
│                  (認證、限流、路由)                               │
└─────────────────────────────────────────────────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        ▼                     ▼                     ▼
┌──────────────┐    ┌──────────────┐    ┌──────────────┐
│   MLflow     │    │    DVC       │    │   Training   │
│   Tracking   │    │   Server     │    │   Scheduler  │
│   Server     │    │              │    │              │
└──────────────┘    └──────────────┘    └──────────────┘
        │                     │                     │
        ▼                     ▼                     ▼
┌──────────────────────────────────────────────────────────┐
│                    Metadata Store                         │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐        │
│  │ Experiments│  │  Datasets  │  │   Models   │        │
│  │   Metrics  │  │  Versions  │  │  Registry  │        │
│  └────────────┘  └────────────┘  └────────────┘        │
│                (PostgreSQL)                               │
└──────────────────────────────────────────────────────────┘
        │                     │                     │
        ▼                     ▼                     ▼
┌──────────────────────────────────────────────────────────┐
│                   Training Cluster                        │
│  ┌────────────────────────────────────────────────┐      │
│  │         Kubernetes Cluster                      │      │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐     │      │
│  │  │Training  │  │Training  │  │Training  │     │      │
│  │  │ Pod 1    │  │ Pod 2    │  │ Pod 3    │ ... │      │
│  │  │ 4× GPU   │  │ 4× GPU   │  │ 4× GPU   │     │      │
│  │  └──────────┘  └──────────┘  └──────────┘     │      │
│  └────────────────────────────────────────────────┘      │
└──────────────────────────────────────────────────────────┘
                              │
                              ▼
┌──────────────────────────────────────────────────────────┐
│                      Storage Layer                        │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐              │
│  │   S3     │  │  MinIO   │  │   NFS    │              │
│  │ (Models) │  │ (Datasets│  │ (Shared) │              │
│  └──────────┘  └──────────┘  └──────────┘              │
└──────────────────────────────────────────────────────────┘
                              │
                              ▼
┌──────────────────────────────────────────────────────────┐
│                   Monitoring & Alerting                   │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐              │
│  │Prometheus│  │ Grafana  │  │AlertMgr  │              │
│  └──────────┘  └──────────┘  └──────────┘              │
└──────────────────────────────────────────────────────────┘
```

### 技術棧

| 層級 | 技術選型 | 原因 |
|------|----------|------|
| **實驗追蹤** | MLflow | 開源、成熟、整合完整 |
| **資料版本控制** | DVC | Git-like 介面、支援大檔案 |
| **訓練框架** | PyTorch Lightning | 簡化分散式訓練、豐富回調 |
| **超參數優化** | Optuna | TPE 演算法、提前停止 |
| **容器編排** | Kubernetes | 資源調度、彈性擴展 |
| **GPU 調度** | NVIDIA GPU Operator | GPU 資源管理 |
| **工作流引擎** | Kubeflow Pipelines | ML Pipeline 編排 |
| **元資料庫** | PostgreSQL | 結構化資料、ACID |
| **物件儲存** | MinIO / S3 | 大型檔案儲存 |
| **監控** | Prometheus + Grafana | 指標收集與視覺化 |

## 資料庫設計

### 1. 實驗表 (experiments)

```sql
CREATE TABLE experiments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(200) NOT NULL,
    artifact_location VARCHAR(500),  -- S3/MinIO 路徑
    lifecycle_stage VARCHAR(20),     -- 'active', 'deleted'
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_experiments_name ON experiments(name);
```

### 2. 訓練執行表 (runs)

```sql
CREATE TABLE runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    experiment_id UUID NOT NULL REFERENCES experiments(id),
    name VARCHAR(200),
    source_type VARCHAR(50),         -- 'notebook', 'project', 'local'
    source_name VARCHAR(500),        -- 原始碼位置
    user_id UUID REFERENCES users(id),
    status VARCHAR(20) NOT NULL,     -- 'running', 'finished', 'failed', 'killed'
    start_time TIMESTAMP NOT NULL,
    end_time TIMESTAMP,
    artifact_uri VARCHAR(500),       -- 產出物位置
    lifecycle_stage VARCHAR(20),
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_runs_experiment_id ON runs(experiment_id);
CREATE INDEX idx_runs_status ON runs(status);
CREATE INDEX idx_runs_user_id ON runs(user_id);
CREATE INDEX idx_runs_start_time ON runs(start_time);
```

### 3. 參數表 (params)

```sql
CREATE TABLE params (
    run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    key VARCHAR(250) NOT NULL,
    value TEXT NOT NULL,
    PRIMARY KEY (run_id, key)
);

CREATE INDEX idx_params_key ON params(key);
```

### 4. 指標表 (metrics)

```sql
CREATE TABLE metrics (
    id BIGSERIAL PRIMARY KEY,
    run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    key VARCHAR(250) NOT NULL,
    value DOUBLE PRECISION NOT NULL,
    timestamp BIGINT NOT NULL,       -- Unix timestamp (ms)
    step BIGINT NOT NULL DEFAULT 0   -- 訓練步數
);

CREATE INDEX idx_metrics_run_id ON metrics(run_id);
CREATE INDEX idx_metrics_key ON metrics(key);
CREATE INDEX idx_metrics_run_key_step ON metrics(run_id, key, step);
```

### 5. 標籤表 (tags)

```sql
CREATE TABLE tags (
    run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    key VARCHAR(250) NOT NULL,
    value TEXT NOT NULL,
    PRIMARY KEY (run_id, key)
);

CREATE INDEX idx_tags_key_value ON tags(key, value);
```

### 6. 模型註冊表 (registered_models)

```sql
CREATE TABLE registered_models (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(256) NOT NULL UNIQUE,
    description TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);
```

### 7. 模型版本表 (model_versions)

```sql
CREATE TABLE model_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    model_id UUID NOT NULL REFERENCES registered_models(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    run_id UUID REFERENCES runs(id),
    stage VARCHAR(20) NOT NULL,      -- 'None', 'Staging', 'Production', 'Archived'
    source VARCHAR(500),             -- 模型檔案位置
    description TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),

    UNIQUE(model_id, version)
);

CREATE INDEX idx_model_versions_model_id ON model_versions(model_id);
CREATE INDEX idx_model_versions_stage ON model_versions(stage);
CREATE INDEX idx_model_versions_run_id ON model_versions(run_id);
```

### 8. 資料集版本表 (dataset_versions)

```sql
CREATE TABLE dataset_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(200) NOT NULL,
    version VARCHAR(100) NOT NULL,   -- Git commit hash 或版本號
    storage_path VARCHAR(500) NOT NULL,
    size_bytes BIGINT,
    file_count INTEGER,
    md5_hash VARCHAR(32),            -- 資料集雜湊
    metadata JSONB,
    created_by UUID REFERENCES users(id),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),

    UNIQUE(name, version)
);

CREATE INDEX idx_dataset_versions_name ON dataset_versions(name);
CREATE INDEX idx_dataset_versions_created_at ON dataset_versions(created_at);
```

### 9. 訓練任務表 (training_jobs)

```sql
CREATE TABLE training_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(200) NOT NULL,
    run_id UUID REFERENCES runs(id),
    job_type VARCHAR(50) NOT NULL,   -- 'single_gpu', 'multi_gpu', 'distributed'
    num_gpus INTEGER,
    num_nodes INTEGER,
    status VARCHAR(20) NOT NULL,     -- 'pending', 'running', 'completed', 'failed'
    k8s_job_name VARCHAR(200),       -- Kubernetes Job 名稱
    config JSONB,                    -- 訓練配置
    logs_uri VARCHAR(500),           -- 日誌位置
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    started_at TIMESTAMP,
    completed_at TIMESTAMP
);

CREATE INDEX idx_training_jobs_status ON training_jobs(status);
CREATE INDEX idx_training_jobs_created_at ON training_jobs(created_at);
```

## 核心功能實作

### 1. MLflow 整合

```python
# training_platform/mlflow_integration.py
import mlflow
import mlflow.pytorch
from typing import Dict, Any

class MLflowTracker:
    def __init__(self, tracking_uri: str, experiment_name: str):
        mlflow.set_tracking_uri(tracking_uri)
        mlflow.set_experiment(experiment_name)
        self.active_run = None

    def start_run(self, run_name: str = None, tags: Dict[str, str] = None):
        """開始新的訓練執行"""
        self.active_run = mlflow.start_run(run_name=run_name, tags=tags)
        return self.active_run

    def log_params(self, params: Dict[str, Any]):
        """記錄超參數"""
        mlflow.log_params(params)

    def log_metrics(self, metrics: Dict[str, float], step: int = None):
        """記錄訓練指標"""
        for key, value in metrics.items():
            mlflow.log_metric(key, value, step=step)

    def log_model(self, model, artifact_path: str, **kwargs):
        """儲存模型"""
        mlflow.pytorch.log_model(model, artifact_path, **kwargs)

    def log_artifact(self, local_path: str):
        """上傳產出物（圖表、檔案等）"""
        mlflow.log_artifact(local_path)

    def set_tag(self, key: str, value: str):
        """設定標籤"""
        mlflow.set_tag(key, value)

    def end_run(self, status: str = "FINISHED"):
        """結束訓練執行"""
        mlflow.end_run(status=status)

# 使用範例
tracker = MLflowTracker(
    tracking_uri="http://mlflow-server:5000",
    experiment_name="recommendation-model-v2"
)

# 開始訓練
with tracker.start_run(run_name="baseline-model", tags={"author": "emma", "version": "v1.0"}):
    # 記錄參數
    tracker.log_params({
        "learning_rate": 0.001,
        "batch_size": 64,
        "epochs": 100,
        "optimizer": "Adam"
    })

    # 訓練迴圈
    for epoch in range(100):
        train_loss = train_one_epoch(model, train_loader)
        val_loss, val_acc = evaluate(model, val_loader)

        # 記錄每個 epoch 的指標
        tracker.log_metrics({
            "train_loss": train_loss,
            "val_loss": val_loss,
            "val_accuracy": val_acc
        }, step=epoch)

    # 儲存最終模型
    tracker.log_model(model, "model")

    # 上傳視覺化圖表
    plot_training_curves(history)
    tracker.log_artifact("training_curves.png")
```

### 2. 分散式訓練

```python
# training_platform/distributed_trainer.py
import pytorch_lightning as pl
from torch.utils.data import DataLoader, DistributedSampler

class DistributedTrainer:
    def __init__(self, model, config):
        self.model = model
        self.config = config

    def setup_data_parallel(self, train_dataset, val_dataset):
        """設定資料並行訓練"""
        # 分散式資料載入器
        train_sampler = DistributedSampler(train_dataset)
        val_sampler = DistributedSampler(val_dataset, shuffle=False)

        train_loader = DataLoader(
            train_dataset,
            batch_size=self.config['batch_size'],
            sampler=train_sampler,
            num_workers=4,
            pin_memory=True
        )

        val_loader = DataLoader(
            val_dataset,
            batch_size=self.config['batch_size'],
            sampler=val_sampler,
            num_workers=4,
            pin_memory=True
        )

        return train_loader, val_loader

    def train_ddp(self, train_loader, val_loader):
        """使用 DDP (Distributed Data Parallel) 訓練"""
        # MLflow 回調
        mlflow_logger = pl.loggers.MLFlowLogger(
            experiment_name=self.config['experiment_name'],
            tracking_uri=self.config['mlflow_uri']
        )

        # Checkpoint 回調
        checkpoint_callback = pl.callbacks.ModelCheckpoint(
            dirpath='checkpoints/',
            filename='{epoch}-{val_loss:.2f}',
            save_top_k=3,
            monitor='val_loss',
            mode='min'
        )

        # Early Stopping 回調
        early_stop_callback = pl.callbacks.EarlyStopping(
            monitor='val_loss',
            patience=10,
            mode='min'
        )

        # Trainer 配置
        trainer = pl.Trainer(
            max_epochs=self.config['epochs'],
            devices=self.config['num_gpus'],      # GPU 數量
            num_nodes=self.config['num_nodes'],   # 節點數量
            strategy='ddp',                       # DDP 策略
            accelerator='gpu',
            precision=16,                         # 混合精度訓練
            logger=mlflow_logger,
            callbacks=[checkpoint_callback, early_stop_callback],
            gradient_clip_val=1.0,               # 梯度裁剪
            accumulate_grad_batches=2             # 梯度累積
        )

        # 開始訓練
        trainer.fit(self.model, train_loader, val_loader)

        return trainer

    def train_deepspeed(self, train_loader, val_loader):
        """使用 DeepSpeed 進行大模型訓練"""
        trainer = pl.Trainer(
            max_epochs=self.config['epochs'],
            devices=self.config['num_gpus'],
            num_nodes=self.config['num_nodes'],
            strategy='deepspeed_stage_3',  # DeepSpeed ZeRO Stage 3
            accelerator='gpu',
            precision=16
        )

        trainer.fit(self.model, train_loader, val_loader)
        return trainer

# Kubernetes Job 配置生成
def generate_k8s_job(config):
    """生成 Kubernetes 訓練任務配置"""
    job_yaml = f"""
apiVersion: batch/v1
kind: Job
metadata:
  name: training-job-{config['run_id']}
spec:
  template:
    spec:
      containers:
      - name: pytorch
        image: pytorch/pytorch:2.0.0-cuda11.7-cudnn8-runtime
        command: ["python", "train.py"]
        args:
          - --experiment-name={config['experiment_name']}
          - --run-id={config['run_id']}
          - --epochs={config['epochs']}
          - --batch-size={config['batch_size']}
        resources:
          limits:
            nvidia.com/gpu: {config['num_gpus']}
            memory: "32Gi"
            cpu: "8"
        volumeMounts:
        - name: dataset
          mountPath: /data
        - name: model-output
          mountPath: /models
      volumes:
      - name: dataset
        persistentVolumeClaim:
          claimName: training-data-pvc
      - name: model-output
        persistentVolumeClaim:
          claimName: model-output-pvc
      restartPolicy: OnFailure
"""
    return job_yaml
```

### 3. 超參數優化

```python
# training_platform/hyperparameter_tuning.py
import optuna
from optuna.integration import PyTorchLightningPruningCallback
import mlflow

class HyperparameterTuner:
    def __init__(self, model_class, train_func, config):
        self.model_class = model_class
        self.train_func = train_func
        self.config = config

    def objective(self, trial):
        """Optuna 目標函數"""
        # 1. 定義搜尋空間
        params = {
            'learning_rate': trial.suggest_float('lr', 1e-5, 1e-2, log=True),
            'batch_size': trial.suggest_categorical('batch_size', [16, 32, 64, 128]),
            'hidden_dim': trial.suggest_int('hidden_dim', 128, 512),
            'num_layers': trial.suggest_int('num_layers', 2, 6),
            'dropout': trial.suggest_float('dropout', 0.1, 0.5),
            'weight_decay': trial.suggest_float('weight_decay', 1e-6, 1e-3, log=True)
        }

        # 2. 建立模型
        model = self.model_class(**params)

        # 3. 訓練（帶提前停止）
        pruning_callback = PyTorchLightningPruningCallback(trial, monitor='val_loss')

        trainer = pl.Trainer(
            max_epochs=self.config['max_epochs'],
            devices=1,
            callbacks=[pruning_callback],
            enable_progress_bar=False,
            enable_model_summary=False
        )

        try:
            trainer.fit(model, self.train_loader, self.val_loader)
            val_loss = trainer.callback_metrics['val_loss'].item()
        except optuna.TrialPruned:
            raise

        # 4. 記錄到 MLflow
        with mlflow.start_run(nested=True):
            mlflow.log_params(params)
            mlflow.log_metric('val_loss', val_loss)

        return val_loss

    def run(self, n_trials=100):
        """執行超參數優化"""
        # 建立 study
        study = optuna.create_study(
            study_name=f"optuna-{self.config['experiment_name']}",
            direction='minimize',
            storage='postgresql://user:pass@localhost/optuna',  # 共享資料庫
            load_if_exists=True,
            sampler=optuna.samplers.TPESampler(),
            pruner=optuna.pruners.MedianPruner(n_startup_trials=5)
        )

        # 開始優化
        study.optimize(self.objective, n_trials=n_trials, n_jobs=4)

        # 最佳結果
        print(f"最佳參數: {study.best_params}")
        print(f"最佳 val_loss: {study.best_value}")

        # 視覺化
        optuna.visualization.plot_optimization_history(study).write_html('optimization_history.html')
        optuna.visualization.plot_param_importances(study).write_html('param_importances.html')

        return study.best_params

# 使用範例
tuner = HyperparameterTuner(
    model_class=MyModel,
    train_func=train_model,
    config={'experiment_name': 'hyperparameter-tuning', 'max_epochs': 50}
)

best_params = tuner.run(n_trials=100)

# 用最佳參數重新訓練
final_model = MyModel(**best_params)
final_trainer = train_model(final_model, epochs=200)
```

### 4. 模型監控與重訓練

```python
# training_platform/model_monitoring.py
import numpy as np
from scipy import stats
from evidently.dashboard import Dashboard
from evidently.tabs import DataDriftTab

class ModelMonitor:
    def __init__(self, model_name, baseline_data):
        self.model_name = model_name
        self.baseline_data = baseline_data
        self.baseline_predictions = None
        self.baseline_metrics = {}

    def detect_data_drift(self, current_data, features):
        """檢測資料漂移"""
        drift_report = {}

        for feature in features:
            # Kolmogorov-Smirnov 檢驗
            baseline_values = self.baseline_data[feature]
            current_values = current_data[feature]

            statistic, p_value = stats.ks_2samp(baseline_values, current_values)

            drift_report[feature] = {
                'statistic': statistic,
                'p_value': p_value,
                'drift_detected': p_value < 0.05
            }

        return drift_report

    def detect_concept_drift(self, model, current_data, current_labels):
        """檢測概念漂移（模型效能下降）"""
        predictions = model.predict(current_data)
        current_metrics = calculate_metrics(current_labels, predictions)

        degradation = {}
        for metric, baseline_value in self.baseline_metrics.items():
            current_value = current_metrics[metric]
            degradation[metric] = baseline_value - current_value

        return degradation, current_metrics

    def should_retrain(self, drift_report, degradation):
        """判斷是否需要重新訓練"""
        # 條件 1: 超過 30% 的特徵發生漂移
        drift_ratio = sum(1 for r in drift_report.values() if r['drift_detected']) / len(drift_report)
        if drift_ratio > 0.3:
            return True, "資料漂移比例過高"

        # 條件 2: 準確度下降超過 5%
        if degradation.get('accuracy', 0) > 0.05:
            return True, "準確度下降超過 5%"

        # 條件 3: F1 分數下降超過 3%
        if degradation.get('f1', 0) > 0.03:
            return True, "F1 分數下降超過 3%"

        return False, None

class AutoRetrainPipeline:
    def __init__(self, model_name, train_func, monitor: ModelMonitor):
        self.model_name = model_name
        self.train_func = train_func
        self.monitor = monitor
        self.mlflow_client = mlflow.tracking.MlflowClient()

    def run_monitoring_cycle(self):
        """執行一個監控週期"""
        # 1. 載入當前生產模型
        model = self.load_production_model()

        # 2. 收集最近的資料
        current_data, current_labels = self.collect_recent_data(days=7)

        # 3. 檢測資料漂移
        drift_report = self.monitor.detect_data_drift(
            current_data,
            features=model.feature_names
        )

        # 4. 檢測概念漂移
        degradation, current_metrics = self.monitor.detect_concept_drift(
            model,
            current_data,
            current_labels
        )

        # 5. 記錄監控指標
        self.log_monitoring_metrics(drift_report, degradation, current_metrics)

        # 6. 判斷是否重新訓練
        should_retrain, reason = self.monitor.should_retrain(drift_report, degradation)

        if should_retrain:
            print(f"觸發自動重新訓練: {reason}")
            self.trigger_retraining(current_data, current_labels)

    def trigger_retraining(self, new_data, new_labels):
        """觸發重新訓練"""
        # 1. 準備訓練資料（歷史資料 + 新資料）
        train_data = self.combine_datasets(self.historical_data, new_data)
        train_labels = self.combine_datasets(self.historical_labels, new_labels)

        # 2. 開始新的 MLflow run
        with mlflow.start_run(run_name=f"auto-retrain-{datetime.now().isoformat()}"):
            mlflow.set_tag("trigger", "auto-retrain")
            mlflow.set_tag("reason", reason)

            # 3. 訓練新模型
            new_model = self.train_func(train_data, train_labels)

            # 4. 評估新模型
            new_metrics = evaluate_model(new_model, self.val_data, self.val_labels)

            # 5. 與當前模型比較
            if self.is_better_model(new_metrics, self.monitor.baseline_metrics):
                # 6. 註冊新模型版本
                self.register_new_model_version(new_model)

                # 7. 部署到 Staging
                self.deploy_to_staging(new_model)

                # 8. A/B Testing
                ab_result = self.run_ab_test(days=3)

                if ab_result['new_model_better']:
                    # 9. 推廣到 Production
                    self.promote_to_production()
                else:
                    print("A/B Testing 顯示新模型表現不佳，保留舊模型")
            else:
                print("新模型表現不如當前模型，取消部署")

    def load_production_model(self):
        """載入當前生產環境的模型"""
        model_version = self.mlflow_client.get_latest_versions(
            self.model_name,
            stages=["Production"]
        )[0]

        model = mlflow.pyfunc.load_model(f"models:/{self.model_name}/Production")
        return model

    def register_new_model_version(self, model):
        """註冊新模型版本"""
        mlflow.sklearn.log_model(model, "model")

        run_id = mlflow.active_run().info.run_id
        model_uri = f"runs:/{run_id}/model"

        self.mlflow_client.create_model_version(
            name=self.model_name,
            source=model_uri,
            run_id=run_id
        )

    def promote_to_production(self):
        """推廣到生產環境"""
        # 取得最新版本
        latest_version = self.mlflow_client.get_latest_versions(
            self.model_name,
            stages=["Staging"]
        )[0]

        # 推廣到 Production
        self.mlflow_client.transition_model_version_stage(
            name=self.model_name,
            version=latest_version.version,
            stage="Production"
        )

        print(f"模型 {self.model_name} v{latest_version.version} 已推廣至 Production")
```

## API 文件

### 1. 建立實驗

```http
POST /api/v1/experiments
Content-Type: application/json
Authorization: Bearer <token>

{
    "name": "recommendation-model-v2",
    "artifact_location": "s3://mlflow-bucket/experiments"
}

Response 201 Created:
{
    "experiment_id": "550e8400-e29b-41d4-a716-446655440000",
    "name": "recommendation-model-v2",
    "artifact_location": "s3://mlflow-bucket/experiments/550e8400"
}
```

### 2. 提交訓練任務

```http
POST /api/v1/training/jobs
Content-Type: application/json
Authorization: Bearer <token>

{
    "experiment_id": "550e8400-e29b-41d4-a716-446655440000",
    "job_type": "distributed",
    "num_gpus": 4,
    "num_nodes": 2,
    "config": {
        "learning_rate": 0.001,
        "batch_size": 64,
        "epochs": 100,
        "dataset_version": "v1.2.3"
    },
    "image": "myregistry/training:v1.0.0",
    "command": ["python", "train.py"]
}

Response 201 Created:
{
    "job_id": "660e8400-e29b-41d4-a716-446655440000",
    "run_id": "770e8400-e29b-41d4-a716-446655440000",
    "status": "pending",
    "k8s_job_name": "training-job-770e8400"
}
```

### 3. 查詢訓練狀態

```http
GET /api/v1/training/jobs/{job_id}
Authorization: Bearer <token>

Response 200 OK:
{
    "job_id": "660e8400-e29b-41d4-a716-446655440000",
    "run_id": "770e8400-e29b-41d4-a716-446655440000",
    "status": "running",
    "progress": {
        "current_epoch": 45,
        "total_epochs": 100,
        "train_loss": 0.234,
        "val_loss": 0.312,
        "val_accuracy": 0.876
    },
    "resource_usage": {
        "gpu_utilization": 0.92,
        "memory_used_gb": 28.5
    },
    "started_at": "2025-01-15T10:00:00Z",
    "estimated_completion": "2025-01-15T14:30:00Z"
}
```

### 4. 記錄實驗指標

```http
POST /api/v1/runs/{run_id}/metrics
Content-Type: application/json
Authorization: Bearer <token>

{
    "metrics": [
        {"key": "train_loss", "value": 0.234, "step": 45},
        {"key": "val_loss", "value": 0.312, "step": 45},
        {"key": "val_accuracy", "value": 0.876, "step": 45}
    ]
}

Response 200 OK:
{
    "message": "Metrics logged successfully"
}
```

### 5. 註冊模型

```http
POST /api/v1/models
Content-Type: application/json
Authorization: Bearer <token>

{
    "name": "recommendation-model",
    "run_id": "770e8400-e29b-41d4-a716-446655440000",
    "artifact_path": "model",
    "description": "Collaborative filtering model v2.0"
}

Response 201 Created:
{
    "model_id": "880e8400-e29b-41d4-a716-446655440000",
    "version": 1,
    "stage": "None"
}
```

### 6. 更新模型階段

```http
PATCH /api/v1/models/{model_id}/versions/{version}
Content-Type: application/json
Authorization: Bearer <token>

{
    "stage": "Production"
}

Response 200 OK:
{
    "model_id": "880e8400-e29b-41d4-a716-446655440000",
    "version": 1,
    "stage": "Production",
    "updated_at": "2025-01-15T15:00:00Z"
}
```

### 7. 查詢實驗比較

```http
GET /api/v1/experiments/{experiment_id}/runs/compare
Authorization: Bearer <token>

Query Parameters:
  - metric=val_accuracy
  - order=desc
  - limit=10

Response 200 OK:
{
    "runs": [
        {
            "run_id": "770e8400-e29b-41d4-a716-446655440000",
            "run_name": "deep-model",
            "params": {"lr": 0.001, "batch_size": 64},
            "metrics": {"val_accuracy": 0.892},
            "duration_seconds": 3600
        },
        {
            "run_id": "990e8400-e29b-41d4-a716-446655440000",
            "run_name": "baseline",
            "params": {"lr": 0.01, "batch_size": 32},
            "metrics": {"val_accuracy": 0.876},
            "duration_seconds": 1800
        }
    ]
}
```

## 效能優化

### 1. 資料載入優化

```python
# 高效資料載入器
class OptimizedDataLoader:
    def __init__(self, dataset, batch_size, num_workers=4):
        self.dataloader = DataLoader(
            dataset,
            batch_size=batch_size,
            num_workers=num_workers,
            pin_memory=True,           # 固定記憶體，加速 GPU 傳輸
            prefetch_factor=2,         # 預取資料
            persistent_workers=True    # 保持 worker 活著
        )

    def __iter__(self):
        return iter(self.dataloader)

# 效能提升:
# - 無優化: GPU 利用率 60%, 訓練時間 10 小時
# - 優化後: GPU 利用率 95%, 訓練時間 6.3 小時 (37% 提升)
```

### 2. 混合精度訓練

```python
# 使用 PyTorch Lightning 自動混合精度
trainer = pl.Trainer(
    precision=16,  # FP16
    # 或 precision='bf16' for BFloat16
)

# 記憶體節省: 50%
# 訓練速度: 提升 2-3倍 (在支援 Tensor Core 的 GPU 上)
# 準確度影響: < 0.1%
```

### 3. 梯度累積

```python
# 模擬大 batch size
trainer = pl.Trainer(
    accumulate_grad_batches=4  # 每 4 個 batch 更新一次
)

# 範例:
# 真實 batch_size=32
# 累積 4 個 batch
# 等效 batch_size=128（但記憶體使用只需 batch_size=32）
```

### 4. Checkpoint 優化

```python
# 只儲存最佳模型
checkpoint_callback = ModelCheckpoint(
    save_top_k=1,          # 只保留最好的 1 個
    monitor='val_loss',
    mode='min',
    save_weights_only=True  # 只儲存權重，不儲存優化器狀態
)

# 儲存空間節省: 70%
# 單個 checkpoint: 2GB → 600MB
```

## 部署架構

```yaml
# mlflow-server-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mlflow-server
spec:
  replicas: 3
  selector:
    matchLabels:
      app: mlflow-server
  template:
    spec:
      containers:
      - name: mlflow
        image: ghcr.io/mlflow/mlflow:v2.9.0
        command:
          - mlflow
          - server
          - --backend-store-uri=postgresql://user:pass@postgres:5432/mlflow
          - --default-artifact-root=s3://mlflow-artifacts/
          - --host=0.0.0.0
          - --port=5000
        env:
        - name: AWS_ACCESS_KEY_ID
          valueFrom:
            secretKeyRef:
              name: s3-credentials
              key: access-key
        - name: AWS_SECRET_ACCESS_KEY
          valueFrom:
            secretKeyRef:
              name: s3-credentials
              key: secret-key
        resources:
          requests:
            memory: "2Gi"
            cpu: "1000m"
          limits:
            memory: "4Gi"
            cpu: "2000m"

---
# GPU 訓練節點池 (Node Pool)
apiVersion: v1
kind: Node
metadata:
  labels:
    node-type: gpu-training
    gpu-type: nvidia-a100
spec:
  taints:
  - key: nvidia.com/gpu
    value: "true"
    effect: NoSchedule

---
# 訓練 Job 範本
apiVersion: batch/v1
kind: Job
metadata:
  name: training-job
spec:
  template:
    spec:
      nodeSelector:
        node-type: gpu-training
      containers:
      - name: trainer
        image: training:v1.0.0
        resources:
          limits:
            nvidia.com/gpu: 8  # 8× A100
      tolerations:
      - key: nvidia.com/gpu
        operator: Equal
        value: "true"
        effect: NoSchedule
```

## 成本估算

### 每月運營成本（100 位資料科學家，每人每週 10 次訓練）

| 項目 | 用量 | 單價 | 月成本 |
|------|------|------|--------|
| **運算資源** | | | |
| GPU 訓練節點 | 20 × A100 (80GB) | $3.00/hr | $43,200 |
| CPU 節點 (MLflow) | 5 × c5.2xlarge | $0.34/hr | $1,224 |
| **儲存** | | | |
| S3 (模型/產出物) | 10TB | $0.023/GB | $230 |
| EBS (資料集) | 50TB SSD | $0.10/GB | $5,000 |
| PostgreSQL (RDS) | db.r5.2xlarge | $0.504/hr | $365 |
| **網路** | | | |
| Data Transfer | 20TB | $0.09/GB | $1,800 |
| **監控** | | | |
| Prometheus + Grafana | - | - | $200 |
| **總計** | | | **$52,019** |

### 成本優化策略

**優化後成本：$31,211（降低 40%）**

1. **Spot Instances for Training**：GPU 成本降低 60-70% = 節省 $25,920
2. **自動擴展**：閒置時段（夜間、週末）縮減至 20% = 節省 $8,640
3. **S3 Intelligent-Tiering**：舊模型自動歸檔 = 節省 $138
4. **資料集去重與壓縮**：儲存空間減少 30% = 節省 $1,500
5. **Reserved Instances (RDS)**：1 年預留 = 節省 $146

### ROI 分析

**傳統方式（無 MLOps 平台）：**
- 實驗無法追蹤，重複實驗浪費時間：每人每月浪費 20 小時
- 模型無法重現，返工時間：每人每月 10 小時
- 手動部署，每次 4 小時
- 100 位資料科學家，平均時薪 $100

**浪費成本 = 100 × (20 + 10 + 4) × $100 = $340,000/月**

**有 MLOps 平台：**
- 平台成本：$31,211/月
- 節省時間成本：$340,000/月
- **淨節省：$308,789/月**
- **ROI = 990%**

## 監控與告警

```yaml
# Prometheus 告警規則
groups:
  - name: training_platform
    rules:
      # GPU 利用率過低
      - alert: LowGPUUtilization
        expr: avg(gpu_utilization) < 0.5
        for: 30m
        annotations:
          summary: "GPU 利用率 < 50%，可能有資源浪費"

      # 訓練任務失敗率高
      - alert: HighTrainingFailureRate
        expr: |
          rate(training_jobs_total{status="failed"}[1h])
          / rate(training_jobs_total[1h]) > 0.2
        for: 15m
        annotations:
          summary: "訓練任務失敗率 > 20%"

      # MLflow 服務不可用
      - alert: MLflowDown
        expr: up{job="mlflow-server"} == 0
        for: 5m
        annotations:
          summary: "MLflow Tracking Server 無法連線"

      # 資料漂移檢測
      - alert: DataDriftDetected
        expr: data_drift_score > 0.3
        for: 1h
        annotations:
          summary: "檢測到資料漂移，建議重新訓練模型"
```

## 總結

模型訓練平台讓機器學習工作流程標準化、自動化，核心價值：

| 能力 | 傳統方式 | MLOps 平台 | 提升 |
|------|----------|------------|------|
| **實驗追蹤** | 手動記錄，易遺失 | 自動追蹤，永久保存 | 100% |
| **可重現性** | 難以重現 | 一鍵重現 | 100% |
| **訓練速度** | 單機 | 分散式 | 4-10× |
| **參數調優** | 手動嘗試 | 自動優化 | 5-10× |
| **部署時間** | 4 小時/次 | 15 分鐘/次 | 16× |
| **模型品質** | 無監控 | 持續監控、自動重訓 | +15% accuracy |

透過本章學習，你掌握了：

1. ✅ **MLflow**：實驗追蹤、模型註冊、版本管理
2. ✅ **DVC**：資料版本控制、Pipeline 管理
3. ✅ **分散式訓練**：DDP、DeepSpeed、Pipeline 並行
4. ✅ **超參數優化**：Optuna TPE、提前停止
5. ✅ **持續監控**：資料漂移、概念漂移、自動重訓練
6. ✅ **A/B Testing**：安全上線新模型
7. ✅ **完整 MLOps 流程**：從實驗到生產的閉環

**下一章**：我們將學習 **推薦引擎**，結合協同過濾、深度學習與實時特徵工程，打造個性化推薦系統。
