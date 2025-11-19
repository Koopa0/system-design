# 模型訓練平台設計：從實驗到生產的完整 MLOps

> 本文檔採用蘇格拉底式對話法（Socratic Method）呈現系統設計的思考過程

## Act 1: 機器學習的混亂現狀

**場景**：Emma 的資料科學團隊正在訓練推薦模型，但遇到了很多問題

**Emma**：「我們的 ML 工作流程一團糟！每個資料科學家都在自己的 Jupyter Notebook 上訓練模型，結果無法重現，也不知道哪個模型最好...」

**David**：「這是很典型的 ML 團隊問題。讓我列出你們可能遇到的痛點：」

### 常見問題

**問題 1：實驗追蹤混亂**
```python
# 資料科學家 A 的筆記本
model = train_model(lr=0.001, epochs=10)
# 準確度：87.3%  ← 記在哪裡？用什麼資料？什麼時候訓練的？

# 資料科學家 B 的筆記本（兩週後）
model = train_model(lr=0.01, epochs=20)
# 準確度：88.1%  ← 比上次好？還是資料變了？
```

**Sarah**：「沒有系統化的實驗追蹤，你永遠不知道：」
- 這個模型用了什麼超參數？
- 訓練資料是哪個版本？
- 能重現這次訓練嗎？
- 為什麼這次比上次好？

**問題 2：資料版本控制缺失**
```bash
# 混亂的資料管理
data/
  train.csv              ← 是最新的嗎？
  train_v2.csv           ← v2 有什麼改變？
  train_final.csv        ← 真的 final 嗎？
  train_final_v2.csv     ← ...
  train_really_final.csv ← 😱
```

**Michael**：「Git 可以管理程式碼，但無法有效管理大型資料集（幾 GB 到幾 TB）。」

**問題 3：模型部署困難**
```
資料科學家：「在我的筆記本上跑得很好啊！」
工程師：「但我不知道怎麼把你的 Jupyter Notebook 部署到生產環境...」
資料科學家：「你需要安裝這 20 個套件，版本要完全一樣...」
工程師：「😭」
```

**問題 4：訓練時間過長**
```python
# 單機訓練
model.fit(X_train, y_train, epochs=100)
# 預估時間：48 小時

# 兩天後...
# 發現超參數設錯了，要重新訓練 😭
```

**Emma**：「完全說中了我們的問題！有解決方案嗎？」

**David**：「有！這就是我們要建立的**模型訓練平台**，涵蓋完整的 MLOps 流程。」

## Act 2: MLOps 流程總覽

**Sarah**：「MLOps 就是 ML + DevOps，目標是將機器學習工作流程工程化、自動化。」

### 完整 MLOps 流程

```
1. 資料管理
   原始資料 → 清洗 → 特徵工程 → 版本控制 (DVC)
   ↓

2. 實驗追蹤
   超參數 → 訓練 → 指標記錄 → 模型儲存 (MLflow)
   ↓

3. 模型訓練
   資料載入 → 分散式訓練 → Checkpoint → 評估
   ↓

4. 模型驗證
   離線評估 → A/B Testing → 性能監控
   ↓

5. 模型部署
   打包 → 部署 (Kubernetes) → 版本管理 → 回滾
   ↓

6. 持續監控
   預測品質 → 資料漂移 → 模型降級 → 重新訓練
```

**Michael**：「這個平台需要解決哪些核心問題？」

**David**：「六大核心能力：」

### 1. 資料版本控制 (DVC - Data Version Control)

```bash
# 類似 Git，但針對大型資料集
dvc add data/train.csv
dvc push

# 其他人可以拉取相同版本的資料
dvc pull
```

**為什麼需要？**
- 確保訓練可重現：相同程式碼 + 相同資料 = 相同結果
- 追蹤資料變更：知道每個版本的資料有什麼不同
- 資料血緣追蹤：這個特徵是從哪裡來的？

### 2. 實驗追蹤 (MLflow Tracking)

```python
import mlflow

with mlflow.start_run():
    # 記錄參數
    mlflow.log_param("learning_rate", 0.001)
    mlflow.log_param("epochs", 100)

    # 訓練模型
    model = train_model(lr=0.001, epochs=100)

    # 記錄指標
    mlflow.log_metric("accuracy", 0.873)
    mlflow.log_metric("f1_score", 0.856)

    # 儲存模型
    mlflow.sklearn.log_model(model, "model")
```

**Web UI 可以看到：**
- 所有實驗的參數、指標
- 比較不同實驗
- 視覺化訓練曲線
- 下載任何歷史模型

### 3. 分散式訓練

```python
# 單機訓練：48 小時
model.fit(X_train, y_train)

# 分散式訓練（4 GPU）：12 小時
trainer = pl.Trainer(
    devices=4,
    strategy="ddp",  # Distributed Data Parallel
    accelerator="gpu"
)
trainer.fit(model)
```

**Emma**：「分散式訓練怎麼運作？」

**Michael**：「主要有兩種策略：」

#### 資料並行 (Data Parallelism)
```
原始資料 (1000 筆)
    ↓
分成 4 份（每份 250 筆）
    ↓
GPU 1: 處理 1-250    ┐
GPU 2: 處理 251-500  ├─ 同時計算梯度
GPU 3: 處理 501-750  │
GPU 4: 處理 751-1000 ┘
    ↓
合併梯度 → 更新模型參數
```

#### 模型並行 (Model Parallelism)
```
大型模型（放不進單一 GPU）
    ↓
Layer 1-10  → GPU 1
Layer 11-20 → GPU 2
Layer 21-30 → GPU 3
Layer 31-40 → GPU 4
    ↓
資料依序通過各 GPU
```

### 4. 超參數優化 (Hyperparameter Tuning)

**Sarah**：「與其手動嘗試超參數，不如自動化：」

```python
import optuna

def objective(trial):
    # 定義搜尋空間
    lr = trial.suggest_float("lr", 1e-5, 1e-1, log=True)
    batch_size = trial.suggest_categorical("batch_size", [16, 32, 64, 128])
    dropout = trial.suggest_float("dropout", 0.1, 0.5)

    # 訓練模型
    model = create_model(lr, batch_size, dropout)
    accuracy = train_and_evaluate(model)

    return accuracy

# 自動搜尋最佳參數
study = optuna.create_study(direction="maximize")
study.optimize(objective, n_trials=100)

print(f"最佳參數: {study.best_params}")
print(f"最佳準確度: {study.best_value}")
```

**搜尋策略：**
- **Grid Search**：窮舉所有組合（慢但完整）
- **Random Search**：隨機採樣（快但可能錯過最優解）
- **Bayesian Optimization**：貝葉斯優化（聰明地搜尋）
- **TPE (Tree Parzen Estimator)**：Optuna 預設（效果好）

### 5. 模型部署

```python
# 打包模型為 Docker 容器
FROM python:3.9
COPY model.pkl /app/
COPY requirements.txt /app/
RUN pip install -r requirements.txt
EXPOSE 8080
CMD ["python", "serve.py"]

# 部署到 Kubernetes
kubectl apply -f model-deployment.yaml

# 流量逐步切換（金絲雀部署）
v1.0: 90% 流量
v1.1: 10% 流量  ← 新模型
    ↓ 監控指標
如果 v1.1 表現良好 → 100% 切換
如果 v1.1 有問題 → 回滾
```

### 6. 模型監控

```python
# 監控預測品質
predict_latency = time.time() - start
if predict_latency > 100ms:
    alert("Prediction too slow")

# 監控資料漂移
current_distribution = get_feature_distribution(new_data)
training_distribution = load_reference_distribution()

drift_score = calculate_drift(current_distribution, training_distribution)
if drift_score > threshold:
    alert("Data drift detected - consider retraining")
```

**Emma**：「明白了！這就像是為機器學習建立一條生產線。」

**David**：「沒錯！讓我們深入每個環節的技術細節。」

## Act 3: 實驗追蹤與管理

**Michael**：「實驗追蹤是 MLOps 的核心。讓我們看看如何用 MLflow 系統化管理實驗。」

### MLflow 四大元件

#### 1. MLflow Tracking - 記錄實驗

```python
import mlflow
import mlflow.sklearn
from sklearn.ensemble import RandomForestClassifier

# 設定 tracking server
mlflow.set_tracking_uri("http://mlflow-server:5000")

# 設定實驗名稱
mlflow.set_experiment("recommendation-model-v2")

with mlflow.start_run(run_name="rf-baseline"):
    # 1. 記錄參數
    params = {
        "n_estimators": 100,
        "max_depth": 10,
        "min_samples_split": 5
    }
    mlflow.log_params(params)

    # 2. 訓練模型
    model = RandomForestClassifier(**params)
    model.fit(X_train, y_train)

    # 3. 評估
    train_acc = model.score(X_train, y_train)
    val_acc = model.score(X_val, y_val)

    # 4. 記錄指標
    mlflow.log_metric("train_accuracy", train_acc)
    mlflow.log_metric("val_accuracy", val_acc)

    # 5. 記錄模型
    mlflow.sklearn.log_model(model, "model")

    # 6. 記錄額外資訊
    mlflow.log_artifact("feature_importance.png")
    mlflow.set_tag("model_type", "random_forest")
    mlflow.set_tag("author", "emma")
```

**在 MLflow UI 可以看到：**
```
Experiment: recommendation-model-v2
├─ Run 1: rf-baseline
│  ├─ Parameters: n_estimators=100, max_depth=10
│  ├─ Metrics: train_accuracy=0.95, val_accuracy=0.87
│  └─ Artifacts: model/, feature_importance.png
├─ Run 2: rf-deep
│  ├─ Parameters: n_estimators=200, max_depth=20
│  └─ Metrics: train_accuracy=0.98, val_accuracy=0.86 (過擬合!)
└─ Run 3: rf-optimized
   └─ Metrics: val_accuracy=0.89 (最佳!)
```

#### 2. MLflow Projects - 可重現的執行環境

```yaml
# MLproject 檔案
name: recommendation-model

conda_env: conda.yaml

entry_points:
  main:
    parameters:
      learning_rate: {type: float, default: 0.001}
      epochs: {type: int, default: 100}
      data_path: {type: string}
    command: "python train.py --lr {learning_rate} --epochs {epochs} --data {data_path}"
```

```yaml
# conda.yaml - 環境定義
name: ml-env
dependencies:
  - python=3.9
  - scikit-learn=1.0.2
  - pandas=1.4.0
  - numpy=1.22.0
  - pip:
    - mlflow==2.0.1
```

**執行：**
```bash
# 本地執行
mlflow run . -P learning_rate=0.01

# 遠端執行（在 Kubernetes 上）
mlflow run . --backend kubernetes -P learning_rate=0.01

# 重現歷史實驗
mlflow run git@github.com:org/project.git -v <commit-hash>
```

#### 3. MLflow Models - 模型打包

```python
# 記錄模型時自動生成標準格式
mlflow.sklearn.log_model(
    model,
    "model",
    signature=mlflow.models.signature.infer_signature(X_train, predictions),
    input_example=X_train[:5]
)

# 生成的目錄結構：
# model/
# ├── MLmodel              ← 模型元資料
# ├── model.pkl            ← 實際模型
# ├── conda.yaml           ← 環境依賴
# ├── requirements.txt
# └── python_env.yaml
```

**載入並使用模型：**
```python
# 方式 1：Python 函式
model = mlflow.sklearn.load_model("runs:/<run-id>/model")
predictions = model.predict(X_new)

# 方式 2：啟動 REST API 服務
mlflow models serve -m "runs:/<run-id>/model" -p 5001

# 方式 3：部署到生產環境
mlflow deployments create -t sagemaker -m "runs:/<run-id>/model"
```

#### 4. MLflow Model Registry - 模型版本管理

```python
# 註冊模型
mlflow.register_model(
    "runs:/<run-id>/model",
    "recommendation-model"
)

# 模型版本生命週期
client = mlflow.tracking.MlflowClient()

# Version 1 → Staging
client.transition_model_version_stage(
    name="recommendation-model",
    version=1,
    stage="Staging"
)

# 驗證通過 → Production
client.transition_model_version_stage(
    name="recommendation-model",
    version=1,
    stage="Production"
)

# 新版本上線，舊版本 → Archived
client.transition_model_version_stage(
    name="recommendation-model",
    version=0,
    stage="Archived"
)
```

**Sarah**：「這樣就能清楚追蹤每個模型的狀態了！」

## Act 4: 資料版本控制 (DVC)

**Emma**：「實驗可以追蹤了，但資料怎麼辦？Git 無法處理大檔案。」

**David**：「DVC (Data Version Control) 就是為此設計的！」

### DVC 運作原理

```bash
# 1. 初始化 DVC
dvc init

# 2. 追蹤資料檔案
dvc add data/train.csv

# 生成兩個檔案：
# data/train.csv.dvc  ← 指標檔案（小，可放 Git）
# data/.gitignore     ← 忽略原始檔案
```

**train.csv.dvc 內容：**
```yaml
outs:
- md5: 3c2e5a8f9b7d1c4e6f8a9b0c1d2e3f4a
  size: 1073741824  # 1GB
  path: train.csv
```

```bash
# 3. 推送資料到遠端儲存（S3/GCS/Azure）
dvc remote add -d myremote s3://my-bucket/dvc-storage
dvc push

# 4. 提交到 Git（只提交 .dvc 檔案）
git add data/train.csv.dvc .dvc/config
git commit -m "Add training data v1.0"
git push

# 5. 其他人拉取
git pull
dvc pull  # 自動下載對應版本的資料
```

### DVC Pipeline - 資料處理流程

```yaml
# dvc.yaml - 定義資料處理流程
stages:
  prepare:
    cmd: python prepare.py
    deps:
      - data/raw/users.csv
      - data/raw/items.csv
    outs:
      - data/prepared/dataset.csv

  featurize:
    cmd: python featurize.py
    deps:
      - data/prepared/dataset.csv
      - src/features.py
    outs:
      - data/features/train.pkl
      - data/features/test.pkl

  train:
    cmd: python train.py
    deps:
      - data/features/train.pkl
      - src/model.py
    params:
      - train.learning_rate
      - train.epochs
    metrics:
      - metrics.json:
          cache: false
    outs:
      - models/model.pkl
```

```bash
# 執行整個 pipeline
dvc repro

# DVC 會自動：
# 1. 檢查哪些檔案改變了
# 2. 只重新執行受影響的階段
# 3. 快取中間結果
```

**Michael**：「DVC + Git 的組合：」
```
Git (管理程式碼和小檔案)
├── src/train.py
├── dvc.yaml
└── data/train.csv.dvc  ← 只是指標

DVC (管理大型資料)
└── S3/GCS
    └── train.csv  ← 真實資料（1GB）
```

**優勢：**
- ✅ 完整的資料血緣追蹤
- ✅ 可重現性：程式碼版本 + 資料版本
- ✅ 高效儲存：去重、壓縮
- ✅ 團隊協作：共享資料集

## Act 5: 分散式訓練策略

**Sarah**：「訓練大型模型時，單機可能要跑好幾天。分散式訓練怎麼做？」

**David**：「主要有三種策略：Data Parallelism、Model Parallelism 和 Pipeline Parallelism。」

### 策略 1：資料並行 (Data Parallelism)

```python
import torch
import torch.distributed as dist
from torch.nn.parallel import DistributedDataParallel as DDP

# 初始化分散式環境
dist.init_process_group(backend='nccl')

# 建立模型
model = MyModel().to(device)
model = DDP(model, device_ids=[local_rank])

# 資料分散
sampler = torch.utils.data.distributed.DistributedSampler(dataset)
dataloader = DataLoader(dataset, sampler=sampler, batch_size=32)

# 訓練迴圈
for epoch in range(num_epochs):
    sampler.set_epoch(epoch)  # 確保每個 epoch 資料不同

    for batch in dataloader:
        # 每個 GPU 處理不同的 batch
        outputs = model(batch)
        loss = criterion(outputs, labels)
        loss.backward()

        # DDP 自動同步梯度
        optimizer.step()
        optimizer.zero_grad()
```

**運作流程：**
```
假設 4 個 GPU，batch_size=32

原始 batch (128 筆)
    ↓ 自動分割
GPU 0: batch[0:32]   ┐
GPU 1: batch[32:64]  ├─ 同時前向傳播
GPU 2: batch[64:96]  │
GPU 3: batch[96:128] ┘
    ↓
每個 GPU 計算自己的梯度
    ↓
All-Reduce: 所有梯度求平均
    ↓
每個 GPU 用相同的梯度更新模型
```

**PyTorch Lightning 簡化版本：**
```python
import pytorch_lightning as pl

class MyModel(pl.LightningModule):
    def __init__(self):
        super().__init__()
        self.model = nn.Linear(100, 10)

    def training_step(self, batch, batch_idx):
        x, y = batch
        y_hat = self.model(x)
        loss = F.cross_entropy(y_hat, y)
        return loss

    def configure_optimizers(self):
        return torch.optim.Adam(self.parameters())

# 自動處理分散式訓練
trainer = pl.Trainer(
    devices=4,              # 4 個 GPU
    strategy="ddp",         # Distributed Data Parallel
    accelerator="gpu",
    max_epochs=10
)

trainer.fit(model, train_dataloader)
```

### 策略 2：模型並行 (Model Parallelism)

**Michael**：「當模型太大，無法放進單一 GPU 時使用。」

```python
import torch.nn as nn

class LargeModel(nn.Module):
    def __init__(self):
        super().__init__()
        # 第一部分放 GPU 0
        self.layer1 = nn.Linear(1000, 1000).to('cuda:0')
        self.layer2 = nn.Linear(1000, 1000).to('cuda:0')

        # 第二部分放 GPU 1
        self.layer3 = nn.Linear(1000, 1000).to('cuda:1')
        self.layer4 = nn.Linear(1000, 10).to('cuda:1')

    def forward(self, x):
        # 資料先在 GPU 0 處理
        x = x.to('cuda:0')
        x = F.relu(self.layer1(x))
        x = F.relu(self.layer2(x))

        # 移到 GPU 1 繼續
        x = x.to('cuda:1')
        x = F.relu(self.layer3(x))
        x = self.layer4(x)
        return x
```

**問題：GPU 利用率低！**
```
時間 →
GPU 0: [■■■■■    ]  ← layer1, layer2 運算後閒置
GPU 1: [     ■■■■]  ← 等待 GPU 0 完成
```

### 策略 3：Pipeline 並行

**Emma**：「如何提升利用率？」

**David**：「把 batch 切成 micro-batches，流水線處理！」

```python
from torch.distributed.pipeline.sync import Pipe

# 定義模型各層
layer1 = nn.Linear(1000, 1000).to('cuda:0')
layer2 = nn.Linear(1000, 1000).to('cuda:0')
layer3 = nn.Linear(1000, 1000).to('cuda:1')
layer4 = nn.Linear(1000, 10).to('cuda:1')

model = nn.Sequential(layer1, layer2, layer3, layer4)

# 啟用 Pipeline 並行
model = Pipe(model, chunks=8)  # 把 batch 切成 8 個 micro-batches

# 訓練
for batch in dataloader:
    outputs = model(batch)
    loss = criterion(outputs, labels)
    loss.backward()
```

**流水線處理：**
```
時間 →
       Micro-batch:  1   2   3   4   5   6   7   8
GPU 0 (layer 1-2): [■  ][■  ][■  ][■  ][■  ][■  ][■  ][■  ]
GPU 1 (layer 3-4):    [■  ][■  ][■  ][■  ][■  ][■  ][■  ][■  ]

GPU 利用率大幅提升！
```

### 混合策略：數據 + 模型並行

**對於超大模型（如 GPT-3）：**
```python
# 8 個節點，每個節點 4 個 GPU = 32 GPU
# 模型切成 4 份（模型並行）
# 每份在 8 個 GPU 上做資料並行

trainer = pl.Trainer(
    devices=4,
    num_nodes=8,
    strategy="deepspeed_stage_3",  # 混合策略
    precision=16  # 混合精度訓練，減少記憶體
)
```

**Michael**：「總結分散式訓練策略：」

| 策略 | 適用場景 | 加速比 | 複雜度 |
|------|----------|--------|--------|
| **資料並行** | 模型小，資料多 | 接近線性 (4 GPU ≈ 4x) | 低 |
| **模型並行** | 模型大，放不進單 GPU | 1-2x（通訊開銷大） | 中 |
| **Pipeline 並行** | 模型大 + 需高利用率 | 2-3x | 高 |
| **混合並行** | 超大模型（> 10B 參數） | 10x+ | 很高 |

## Act 6: 超參數優化自動化

**Sarah**：「手動調參太慢了！如何自動化？」

**David**：「Optuna 是目前最強大的超參數優化框架。」

### Optuna 基礎用法

```python
import optuna
from sklearn.ensemble import RandomForestClassifier
from sklearn.model_selection import cross_val_score

def objective(trial):
    # 1. 定義搜尋空間
    params = {
        'n_estimators': trial.suggest_int('n_estimators', 10, 200),
        'max_depth': trial.suggest_int('max_depth', 2, 32),
        'min_samples_split': trial.suggest_int('min_samples_split', 2, 20),
        'min_samples_leaf': trial.suggest_int('min_samples_leaf', 1, 10),
    }

    # 2. 訓練模型
    model = RandomForestClassifier(**params, random_state=42)

    # 3. 交叉驗證評估
    score = cross_val_score(model, X_train, y_train, cv=5, scoring='accuracy').mean()

    return score

# 4. 建立 study 並優化
study = optuna.create_study(
    direction='maximize',  # 最大化準確度
    sampler=optuna.samplers.TPESampler(),  # 使用 TPE 演算法
    pruner=optuna.pruners.MedianPruner()   # 提前停止表現差的試驗
)

study.optimize(objective, n_trials=100)

# 5. 查看結果
print(f"最佳參數: {study.best_params}")
print(f"最佳分數: {study.best_value}")

# 6. 視覺化
optuna.visualization.plot_optimization_history(study)
optuna.visualization.plot_param_importances(study)
```

### 進階功能：提前停止 (Pruning)

```python
import optuna
from pytorch_lightning.callbacks import Callback

class PyTorchLightningPruningCallback(Callback):
    def __init__(self, trial, monitor):
        self.trial = trial
        self.monitor = monitor

    def on_validation_end(self, trainer, pl_module):
        epoch = trainer.current_epoch
        current_score = trainer.callback_metrics.get(self.monitor)

        # 報告當前分數
        self.trial.report(current_score, epoch)

        # 判斷是否該停止
        if self.trial.should_prune():
            raise optuna.TrialPruned()

def objective(trial):
    # 建議超參數
    lr = trial.suggest_float('lr', 1e-5, 1e-1, log=True)
    batch_size = trial.suggest_categorical('batch_size', [16, 32, 64])

    model = MyModel(lr=lr)

    trainer = pl.Trainer(
        max_epochs=50,
        callbacks=[PyTorchLightningPruningCallback(trial, 'val_accuracy')]
    )

    trainer.fit(model)

    return trainer.callback_metrics['val_accuracy'].item()

# 自動停止表現差的試驗，節省時間
study.optimize(objective, n_trials=100)
```

**效果：**
```
試驗 1: epoch 1 acc=0.5 → 繼續
        epoch 2 acc=0.55 → 繼續
        epoch 3 acc=0.60 → 繼續
        ...
        epoch 50 acc=0.85 → 完成

試驗 2: epoch 1 acc=0.3 → 繼續
        epoch 2 acc=0.32 → 遠低於中位數 → 提前停止！

節省時間：50 epoch → 2 epoch
```

### 分散式超參數優化

```python
# 在多台機器上同時搜尋
import optuna

# 使用共享資料庫
study = optuna.create_study(
    study_name='distributed-optimization',
    storage='postgresql://user:pass@host/dbname',
    load_if_exists=True
)

# 機器 A、B、C 同時執行
study.optimize(objective, n_trials=100)

# Optuna 自動協調，避免重複試驗
```

**Michael**：「超參數優化建議：」

```
小模型（訓練快 < 1分鐘）:
→ 使用 Grid Search 或 Random Search，窮舉搜尋

中型模型（訓練 10-60分鐘）:
→ 使用 Optuna TPE，100-200 次試驗

大型模型（訓練 > 1小時）:
→ 使用 Optuna + Pruning，20-50 次試驗
→ 分散式優化，加速搜尋

超大模型（訓練 > 1天）:
→ 手動調整 + 少量關鍵參數自動優化
→ 參考論文的建議值
```

## Act 7: 持續監控與改進

**Emma**：「模型部署後就結束了嗎？」

**David**：「不！這只是開始。模型會隨時間降級，需要持續監控。」

### 模型降級的原因

**1. 資料漂移 (Data Drift)**
```python
# 訓練時的資料分佈
訓練資料（2023 年）：
年齡分佈：平均 35 歲，標準差 12
收入分佈：平均 $50K，標準差 $20K

# 生產環境的資料（2024 年）
新資料：
年齡分佈：平均 42 歲，標準差 15  ← 漂移了！
收入分佈：平均 $60K，標準差 $25K

→ 模型預測準確度下降
```

**2. 概念漂移 (Concept Drift)**
```
疫情前：
購買模式 = f(價格, 品質, 品牌)

疫情後：
購買模式 = f(價格, 品質, 品牌, 是否宅配, 防疫用品)

→ 原本的特徵不夠了，需要重新訓練
```

**3. 標籤分佈改變**
```
原始訓練資料：
正樣本 50%, 負樣本 50%

生產環境：
正樣本 80%, 負樣本 20%  ← 不平衡

→ 模型偏向預測正樣本
```

### 監控指標

```python
from evidently import ColumnMapping
from evidently.dashboard import Dashboard
from evidently.tabs import DataDriftTab, CatTargetDriftTab

# 1. 資料漂移檢測
def detect_data_drift(reference_data, current_data):
    dashboard = Dashboard(tabs=[DataDriftTab()])
    dashboard.calculate(reference_data, current_data)

    report = dashboard.show()

    # 檢查哪些特徵漂移了
    for feature, drift in report['data_drift']['data'].items():
        if drift['drift_detected']:
            print(f"警告：{feature} 發生漂移！")
            print(f"  P-value: {drift['p_value']}")
            print(f"  Drift score: {drift['drift_score']}")

# 2. 模型效能監控
class ModelMonitor:
    def __init__(self, model, threshold=0.05):
        self.model = model
        self.threshold = threshold
        self.baseline_metrics = {}

    def set_baseline(self, X_val, y_val):
        """設定基準指標"""
        preds = self.model.predict(X_val)
        self.baseline_metrics = {
            'accuracy': accuracy_score(y_val, preds),
            'precision': precision_score(y_val, preds),
            'recall': recall_score(y_val, preds),
            'f1': f1_score(y_val, preds)
        }

    def check_performance(self, X_new, y_new):
        """檢查當前效能"""
        preds = self.model.predict(X_new)
        current_metrics = {
            'accuracy': accuracy_score(y_new, preds),
            'precision': precision_score(y_new, preds),
            'recall': recall_score(y_new, preds),
            'f1': f1_score(y_new, preds)
        }

        # 比較
        for metric, baseline in self.baseline_metrics.items():
            current = current_metrics[metric]
            degradation = baseline - current

            if degradation > self.threshold:
                alert(f"{metric} 下降 {degradation:.2%}，考慮重新訓練！")

        return current_metrics

# 3. 預測分佈監控
def monitor_prediction_distribution(model, X_stream):
    """監控預測分佈是否改變"""
    predictions = model.predict_proba(X_stream)

    # 計算預測信心
    confidence = predictions.max(axis=1)

    # 警告：低信心預測過多
    low_confidence_ratio = (confidence < 0.6).mean()
    if low_confidence_ratio > 0.3:
        alert(f"30% 的預測信心 < 0.6，模型可能需要重新訓練")

    # 警告：預測分佈偏斜
    class_distribution = predictions.mean(axis=0)
    if class_distribution.max() > 0.8:
        alert("預測過度偏向某一類別")
```

### 自動重新訓練流程

```python
class AutoRetrainPipeline:
    def __init__(self, model, train_func, threshold=0.05):
        self.model = model
        self.train_func = train_func
        self.threshold = threshold
        self.monitor = ModelMonitor(model, threshold)

    def run(self):
        """持續監控並在需要時重新訓練"""
        while True:
            # 1. 收集新資料
            X_new, y_new = collect_recent_data(days=7)

            # 2. 檢查效能
            metrics = self.monitor.check_performance(X_new, y_new)

            # 3. 檢查資料漂移
            drift_detected = detect_data_drift(
                self.reference_data,
                X_new
            )

            # 4. 決定是否重新訓練
            if should_retrain(metrics, drift_detected):
                print("觸發自動重新訓練...")

                # 5. 準備新的訓練資料（舊資料 + 新資料）
                X_train = combine_data(self.X_train_old, X_new)
                y_train = combine_data(self.y_train_old, y_new)

                # 6. 重新訓練
                new_model = self.train_func(X_train, y_train)

                # 7. A/B Testing
                if ab_test_passed(self.model, new_model):
                    print("新模型表現更好，部署上線")
                    self.model = new_model
                    self.monitor.set_baseline(X_new, y_new)
                else:
                    print("新模型表現不佳，保留舊模型")

            # 8. 等待下一個週期
            time.sleep(86400)  # 每天檢查一次

def should_retrain(metrics, drift_detected):
    """重新訓練觸發條件"""
    # 條件 1：準確度下降超過 5%
    if metrics['accuracy'] < baseline_accuracy - 0.05:
        return True

    # 條件 2：檢測到資料漂移
    if drift_detected:
        return True

    # 條件 3：定期重新訓練（每 30 天）
    if days_since_last_training > 30:
        return True

    return False
```

### A/B Testing 框架

```python
class ABTest:
    def __init__(self, model_a, model_b, traffic_split=0.1):
        self.model_a = model_a  # 當前模型
        self.model_b = model_b  # 新模型
        self.traffic_split = traffic_split  # 10% 流量給新模型
        self.results = {'a': [], 'b': []}

    def predict(self, user_id, features):
        """根據用戶 ID 決定使用哪個模型"""
        # 一致性雜湊，確保同一用戶總是分配到相同模型
        if hash(user_id) % 100 < self.traffic_split * 100:
            model = self.model_b
            group = 'b'
        else:
            model = self.model_a
            group = 'a'

        prediction = model.predict(features)

        # 記錄結果
        self.results[group].append({
            'user_id': user_id,
            'prediction': prediction,
            'timestamp': datetime.now()
        })

        return prediction

    def evaluate(self, min_samples=1000):
        """評估兩個模型的表現"""
        if len(self.results['b']) < min_samples:
            print(f"資料不足，需要 {min_samples} 筆")
            return None

        # 計算指標
        metric_a = calculate_metrics(self.results['a'])
        metric_b = calculate_metrics(self.results['b'])

        # 統計顯著性檢驗
        p_value = statistical_test(metric_a, metric_b)

        if p_value < 0.05 and metric_b > metric_a:
            print("新模型顯著更好！建議全面部署")
            return 'b'
        elif p_value < 0.05 and metric_b < metric_a:
            print("新模型表現較差！保留舊模型")
            return 'a'
        else:
            print("兩個模型無顯著差異")
            return None
```

**Sarah**：「總結 MLOps 完整流程：」

```
資料管理（DVC）
    ↓
實驗追蹤（MLflow）
    ↓
超參數優化（Optuna）
    ↓
分散式訓練（PyTorch Lightning）
    ↓
模型註冊（MLflow Model Registry）
    ↓
A/B Testing
    ↓
部署上線（Kubernetes）
    ↓
持續監控（Evidently, Prometheus）
    ↓
自動重新訓練 ──┘ (循環)
```

**Emma**：「這樣就能建立一個完整的、可持續的機器學習系統了！」

**Michael**：「沒錯！這就是現代化的 MLOps 平台。」

---

## 總結

**David**：「建立模型訓練平台的核心原則：」

| 原則 | 說明 | 工具 |
|------|------|------|
| **可重現性** | 任何實驗都能完整重現 | MLflow + DVC |
| **可追蹤性** | 每個模型的來源清楚可查 | MLflow Tracking |
| **可擴展性** | 從單機到分散式無縫擴展 | PyTorch Lightning |
| **自動化** | 減少手動操作，提升效率 | Optuna + CI/CD |
| **可靠性** | 模型品質穩定，異常可快速回滾 | A/B Testing + Monitoring |

**透過本章學習，你掌握了：**

1. ✅ **實驗管理**：MLflow 追蹤、比較、重現實驗
2. ✅ **資料版本控制**：DVC 管理大型資料集
3. ✅ **分散式訓練**：加速模型訓練 4-10 倍
4. ✅ **超參數優化**：自動搜尋最佳參數
5. ✅ **持續監控**：檢測模型降級，自動重新訓練
6. ✅ **A/B Testing**：安全地上線新模型
7. ✅ **MLOps 流程**：從實驗到生產的完整閉環

**下一章**：我們將學習**推薦引擎**，結合機器學習與系統設計，打造個性化推薦系統。
