# Worker システム詳細設計書

## 概要

このシステムは、Go言語で実装された非同期ジョブ処理システムです。SQLiteをバックエンドとして使用し、複数のワーカーが並行してジョブを処理する分散ワーカーアーキテクチャを採用しています。

## システムアーキテクチャ

### コンポーネント構成

```
┌─────────────────────────────────────────────────────────┐
│                    Application Layer                     │
│  - cmd/server (APIサーバー)                              │
│  - cmd/worker (ワーカープロセス)                         │
│  - cmd/worker-manager (管理ツール)                       │
└─────────────────────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────┐
│                   Service Layer                          │
│  - pkg/jobs/JobQueueService (ジョブキュー管理)          │
│  - pkg/database/DatabaseService (DB接続管理)            │
└─────────────────────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────┐
│                 Data Access Layer                        │
│  - db/queries.sql.go (sqlc生成コード)                   │
│  - db/models.go (データモデル)                           │
└─────────────────────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────┐
│                    Database (SQLite)                     │
│  - users テーブル                                        │
│  - job_queue テーブル                                    │
└─────────────────────────────────────────────────────────┘
```

## データモデル

### job_queue テーブル

| カラム名 | 型 | 説明 |
|---------|-----|------|
| id | INTEGER | 主キー (自動採番) |
| job_type | TEXT | ジョブタイプ ('user_created', 'data_analysis', 等) |
| payload | TEXT | ジョブデータ (JSON形式) |
| status | TEXT | ステータス ('pending', 'processing', 'completed', 'failed') |
| priority | INTEGER | 優先度 (数値が大きいほど高優先度、デフォルト: 0) |
| max_retries | INTEGER | 最大リトライ回数 (デフォルト: 3) |
| retry_count | INTEGER | 現在のリトライ回数 (デフォルト: 0) |
| error_message | TEXT | エラーメッセージ (失敗時) |
| scheduled_at | DATETIME | スケジュール実行時刻 |
| started_at | DATETIME | 処理開始時刻 |
| completed_at | DATETIME | 処理完了時刻 |
| created_at | DATETIME | レコード作成時刻 |

**インデックス:**
- `idx_job_queue_status`: status カラム
- `idx_job_queue_type`: job_type カラム
- `idx_job_queue_scheduled`: scheduled_at カラム
- `idx_job_queue_priority`: priority DESC, scheduled_at (複合インデックス)

## コアコンポーネント

### 1. Worker (`cmd/worker/main.go`)

**責務:** ジョブキューからジョブを取得し、適切なプロセッサーで処理を実行

#### Worker構造体

```go
type Worker struct {
    id           int                  // ワーカーID
    jobQueue     *JobQueueService     // ジョブキューサービス
    stopCh       chan struct{}        // 停止シグナルチャネル
    wg           *sync.WaitGroup      // ワーカー終了待機用
    processingWg *sync.WaitGroup      // 処理中ジョブ完了待機用
}
```

**主要メソッド:**

- `NewWorker(id, jobQueue, wg)`: ワーカーインスタンス生成
- `Start()`: ワーカー起動・メインループ実行
- `Stop()`: グレースフルシャットダウン
- `processNextJob(processors)`: 次のジョブを取得・処理

#### 動作フロー

1. **起動 (Start)**
   - プロセッサーマップを初期化
   - 1秒間隔のティッカーを開始
   - メインループでジョブをポーリング

2. **ジョブ処理 (processNextJob)**
   ```
   ┌──────────────────────────────────────────┐
   │ 1. GetNextJob() でジョブを取得           │
   └──────────────────┬───────────────────────┘
                      │
                      ▼
   ┌──────────────────────────────────────────┐
   │ 2. ステータスを 'processing' に更新      │
   └──────────────────┬───────────────────────┘
                      │
                      ▼
   ┌──────────────────────────────────────────┐
   │ 3. Payloadを JobPayload にデシリアライズ │
   └──────────────────┬───────────────────────┘
                      │
                      ▼
   ┌──────────────────────────────────────────┐
   │ 4. job_type に対応する Processor を取得  │
   └──────────────────┬───────────────────────┘
                      │
                      ▼
   ┌──────────────────────────────────────────┐
   │ 5. processor.Process() を実行            │
   └──────────────────┬───────────────────────┘
                      │
           ┌──────────┴──────────┐
           │                     │
           ▼                     ▼
   ┌──────────────┐     ┌──────────────┐
   │ 成功         │     │ 失敗         │
   │ CompleteJob  │     │ FailJob      │
   └──────────────┘     └──────────────┘
   ```

3. **シャットダウン (Stop)**
   - stopCh を閉じる
   - processingWg.Wait() で処理中のジョブ完了を待機
   - リソースをクリーンアップ

#### 並行処理の仕組み

- **ゴルーチンによる非同期処理**: 各ジョブは別ゴルーチンで処理され、ワーカーは即座に次のジョブをポーリング可能
- **processingWg**: 処理中のジョブを追跡し、グレースフルシャットダウンを実現
- **複数ワーカー並列実行**: デフォルト3ワーカー、環境変数 `WORKER_COUNT` で設定変更可能

### 2. JobProcessor インターフェース

全てのジョブプロセッサーが実装すべきインターフェース:

```go
type JobProcessor interface {
    Process(job *db.JobQueue, payload JobPayload) error
    JobType() JobType
}
```

#### 実装済みプロセッサー

##### UserCreatedProcessor (`cmd/worker/main.go:31-71`)

**目的:** ユーザー作成時の後処理

**処理内容:**
- ウェルカムメール送信シミュレーション
- 追加プロパティ解析 (hobby, location, score 等)
- サインアップメトリクス記録
- プロファイル初期設定

**処理時間:** 約500ms

##### DataAnalysisProcessor (`cmd/worker/main.go:74-89`)

**目的:** データ分析ジョブの実行

**処理内容:**
- 長時間実行される分析処理のシミュレーション
- 分析結果の記録

**処理時間:** 約2秒

##### EmailNotificationProcessor (`cmd/worker/main.go:92-108`)

**目的:** メール通知の送信

**処理内容:**
- 複数受信者へのメール送信
- 配信ログ記録

**処理時間:** 約300ms

### 3. JobQueueService (`pkg/jobs/job-queue.go`)

**責務:** ジョブキューの永続化と状態管理

#### 主要データ型

```go
type JobType string

const (
    JobUserCreated      JobType = "user_created"
    JobDataAnalysis     JobType = "data_analysis"
    JobEmailNotification JobType = "email_notification"
    JobDataExport       JobType = "data_export"
)

type JobPayload struct {
    UserID          *int64                 // ユーザーID
    UserData        map[string]interface{} // ユーザーデータ
    AdditionalProps map[string]interface{} // 追加プロパティ
    Message         string                 // メッセージ
    Recipients      []string               // 受信者リスト
    ValidationMode  string                 // バリデーションモード
}

type JobQueueService struct {
    db      *sql.DB      // データベース接続
    queries *db.Queries  // sqlc生成クエリ
}
```

#### 主要メソッド

##### EnqueueJob (`pkg/jobs/job-queue.go:45-63`)

**シグネチャ:** `EnqueueJob(jobType JobType, payload JobPayload, priority int) (*db.JobQueue, error)`

**処理:**
1. PayloadをJSON文字列にマーシャル
2. job_queue テーブルに新規レコード挿入
3. ステータスは 'pending'、max_retries=3、scheduled_at=現在時刻

##### GetNextJob (`pkg/jobs/job-queue.go:65-88`)

**シグネチャ:** `GetNextJob() (*db.JobQueue, error)`

**処理:**
1. 以下の条件でジョブを検索:
   - status = 'pending'
   - scheduled_at <= 現在時刻
   - retry_count < max_retries
2. ORDER BY: priority DESC, scheduled_at ASC (高優先度・古い順)
3. LIMIT 1
4. ステータスを 'processing' に更新、started_at を記録

**重要:** このメソッドはアトミックではないため、複数ワーカーで同時実行時に競合の可能性あり

##### CompleteJob (`pkg/jobs/job-queue.go:90-99`)

**シグネチャ:** `CompleteJob(jobID int64) error`

**処理:**
- ステータスを 'completed' に更新
- completed_at に現在時刻を記録

##### FailJob (`pkg/jobs/job-queue.go:101-118`)

**シグネチャ:** `FailJob(jobID int64, errorMessage string, retry bool) error`

**処理:**
- **retry=true の場合:**
  - retry_count をインクリメント
  - ステータスを 'pending' に戻す
  - scheduled_at を再計算: `CURRENT_TIMESTAMP + (retry_count + 1) * 5 分`
  - error_message を記録
- **retry=false の場合:**
  - ステータスを 'failed' に更新
  - completed_at に現在時刻を記録
  - error_message を記録

**リトライ戦略:** エクスポネンシャルバックオフ (5分, 10分, 15分...)

##### GetJobStats (`pkg/jobs/job-queue.go:120-126`)

**シグネチャ:** `GetJobStats() (*db.GetJobStatsRow, error)`

**戻り値:**
```go
type GetJobStatsRow struct {
    PendingCount    int64
    ProcessingCount int64
    CompletedCount  int64
    FailedCount     int64
}
```

##### ListJobs (`pkg/jobs/job-queue.go:128-137`)

**シグネチャ:** `ListJobs(status string, limit int) ([]db.JobQueue, error)`

**処理:** 指定ステータスのジョブを作成日時降順で最大limit件取得

### 4. Worker Manager (`cmd/worker-manager/main.go`)

**責務:** ジョブキューの管理・監視用CLIツール

#### コマンド

##### stats
```bash
worker-manager stats [database_path]
```
ジョブキュー統計を表示 (Pending/Processing/Completed/Failed 件数)

##### list
```bash
worker-manager list [database_path] [status]
```
指定ステータスのジョブ一覧を表示 (デフォルト: pending、最大20件)

表示項目:
- ID, Type, Priority, Retries
- Error Message (失敗時)
- Payload プレビュー
- 作成日時

##### enqueue
```bash
worker-manager enqueue [database_path] <job_type> <message> [priority]
```
テストジョブをキューに追加

サポートされるジョブタイプ:
- user_created
- data_analysis
- email_notification
- data_export

##### clear
```bash
worker-manager clear [database_path] [status]
```
指定ステータスのジョブを削除 (現在は未実装)

### 5. DatabaseService (`pkg/database/database.go`)

**責務:** データベース接続・スキーマ初期化・高レベルDB操作

#### 構造体

```go
type DatabaseService struct {
    db       *sql.DB
    queries  *db.Queries
    jobQueue *JobQueueService
}
```

#### 初期化フロー

```go
func NewDatabaseService(dbPath string) (*DatabaseService, error)
```

1. SQLite接続を確立
2. Ping でヘルスチェック
3. initSchema() でテーブル作成 (IF NOT EXISTS)
4. JobQueueService をインスタンス化
5. DatabaseService を返却

#### ユーザー作成時のジョブエンキュー

`CreateUser()` メソッド内で自動的にジョブをエンキュー:

```go
// pkg/database/database.go:132-150
jobPayload := JobPayload{
    UserID:          &user.Id,
    UserData:        map[string]interface{}{...},
    AdditionalProps: additionalProps,
}

_, jobErr := ds.jobQueue.EnqueueJob(JobUserCreated, jobPayload, 1)
```

**重要:** ジョブエンキュー失敗はログ出力のみでユーザー作成自体は失敗させない (Fire-and-forget パターン)

## 実行フロー

### ワーカー起動

```bash
# デフォルト設定 (3ワーカー、workers.db)
go run cmd/worker/main.go

# カスタム設定
WORKER_COUNT=5 go run cmd/worker/main.go /path/to/custom.db
```

**起動シーケンス:**

1. コマンドライン引数からDBパス取得 (デフォルト: workers.db)
2. DatabaseService 初期化
3. 環境変数 WORKER_COUNT 読み取り (デフォルト: 3)
4. N個のワーカーをゴルーチンで起動
5. 30秒ごとにジョブ統計を出力するゴルーチンを起動
6. SIGINT/SIGTERM 待機
7. シグナル受信でグレースフルシャットダウン

### ジョブ処理ライフサイクル

```
[APIサーバー]
    │
    │ POST /users
    │
    ▼
[DatabaseService.CreateUser()]
    │
    │ INSERT INTO users
    │ EnqueueJob(JobUserCreated, ...)
    │
    ▼
[job_queue テーブル]
    status: pending
    priority: 1
    scheduled_at: 現在時刻
    │
    │ (1秒ごとのポーリング)
    │
    ▼
[Worker.processNextJob()]
    │
    │ GetNextJob() → status: processing
    │
    ▼
[JobProcessor.Process()]
    │
    ├─ 成功 → CompleteJob()
    │          status: completed
    │          completed_at: 記録
    │
    └─ 失敗 → FailJob(retry=true)
               status: pending
               retry_count++
               scheduled_at: +5分
               error_message: 記録
```

## エラーハンドリングとリトライ戦略

### リトライ条件

ジョブは以下の条件で自動リトライされる:
- `retry_count < max_retries` (デフォルト: 3)
- Processor.Process() がエラーを返す
- FailJob() で retry=true が指定される

### リトライスケジューリング

```sql
scheduled_at = datetime(CURRENT_TIMESTAMP, '+' || (retry_count + 1) * 5 || ' minutes')
```

- 1回目の失敗: 5分後に再実行
- 2回目の失敗: 10分後に再実行
- 3回目の失敗: 15分後に再実行
- max_retries 超過: status='failed' で終了

### エラー分類

**リトライ対象:**
- 一時的なネットワークエラー
- 外部API のタイムアウト
- リソース不足 (メモリ、接続数等)

**リトライ非対象:**
- Payload のパース失敗 (`cmd/worker/main.go:169`)
- 未知のジョブタイプ (`cmd/worker/main.go:177`)
- バリデーションエラー (ビジネスロジック)

## 並行性とスレッドセーフティ

### 並行制御

1. **複数ワーカープロセス:**
   - 各ワーカーは独立したゴルーチンで実行
   - sync.WaitGroup で終了を同期

2. **ジョブ単位の並行処理:**
   - processNextJob() 内でゴルーチン起動
   - processingWg で処理中のジョブを追跡

3. **データベースレベルの競合:**
   - SQLite の SERIALIZABLE 分離レベルに依存
   - GetNextJob() は LIMIT 1 で単一ジョブのみ取得
   - **注意:** 複数ワーカーで同一ジョブを重複処理する可能性あり (FOR UPDATE 未使用)

### 潜在的な問題点

**競合状態 (Race Condition):**

```
Worker 1: SELECT job WHERE status='pending' LIMIT 1 → job_id=42
Worker 2: SELECT job WHERE status='pending' LIMIT 1 → job_id=42
Worker 1: UPDATE job SET status='processing' WHERE id=42
Worker 2: UPDATE job SET status='processing' WHERE id=42
```

**推奨改善策:**
- 悲観的ロック (SELECT ... FOR UPDATE) ← SQLiteでは未サポート
- 楽観的ロック (バージョン番号またはタイムスタンプチェック)
- Redis等の分散ロックメカニズム導入

## パフォーマンス特性

### スループット

- **ポーリング間隔:** 1秒
- **最大同時実行ジョブ数:** ワーカー数 × (理論上無制限、実際はシステムリソース制約)
- **デフォルト構成 (3ワーカー):** 約3ジョブ/秒 (ポーリングオーバーヘッド考慮)

### レイテンシ

- **ジョブピックアップ遅延:** 最大1秒 (ポーリング間隔)
- **処理時間:** プロセッサー依存
  - UserCreated: ~500ms
  - EmailNotification: ~300ms
  - DataAnalysis: ~2s

### データベースインデックス最適化

```sql
-- 高頻度クエリに対するインデックス
CREATE INDEX idx_job_queue_priority ON job_queue(priority DESC, scheduled_at);
```

GetNextPendingJob クエリで使用され、O(log n) でジョブ検索が可能

## 設定とカスタマイズ

### 環境変数

| 変数名 | 説明 | デフォルト値 |
|--------|------|-------------|
| WORKER_COUNT | 並行ワーカー数 | 3 |

### コマンドライン引数

```bash
worker [database_path]
worker-manager <command> [database_path] [args...]
```

### ジョブパラメータ

ジョブ作成時に指定可能:
- **priority:** 整数値 (大きいほど高優先度)
- **max_retries:** 最大リトライ回数 (EnqueueJob内でハードコード: 3)
- **scheduled_at:** 遅延実行の場合は未来の時刻を指定可能

## 監視とメトリクス

### ログ出力

**起動/停止:**
```
Worker 1 started
Worker 1 received stop signal
Worker 1 stopped
```

**ジョブ処理:**
```
Worker 1: Processing job 42 (type: user_created)
Worker 1: Job 42 completed successfully
Worker 1: Job 43 failed: connection timeout
```

### 統計情報

30秒ごとに自動出力:
```
Job Stats - Pending: 15, Processing: 3, Completed: 142, Failed: 5
```

CLI経由でも取得可能:
```bash
worker-manager stats
```

## 拡張ポイント

### 新しいジョブタイプの追加

1. **JobType定数を追加** (`pkg/jobs/job-queue.go:15-22`)
   ```go
   const (
       JobNewType JobType = "new_type"
   )
   ```

2. **Processorを実装** (`cmd/worker/main.go`)
   ```go
   type NewTypeProcessor struct{}

   func (p *NewTypeProcessor) JobType() JobType {
       return JobNewType
   }

   func (p *NewTypeProcessor) Process(job *db.JobQueue, payload JobPayload) error {
       // 処理ロジック
       return nil
   }
   ```

3. **Workerに登録** (`cmd/worker/main.go:123`)
   ```go
   processors := map[JobType]JobProcessor{
       JobNewType: &NewTypeProcessor{},
   }
   ```

### カスタムスケジューリング戦略

`scheduled_at` を調整することで実現可能:
- **即時実行:** `time.Now()`
- **遅延実行:** `time.Now().Add(1 * time.Hour)`
- **定期実行:** 完了時に新しいジョブをエンキュー (Cronパターン)

### 外部通知システム連携

CompleteJob/FailJob 後にフックを追加:
- Webhook通知
- メトリクス送信 (Prometheus, Datadog等)
- 監査ログ記録

## セキュリティ考慮事項

### Payloadのバリデーション

現在、Payloadのデシリアライズは行うが、スキーマバリデーションは実装されていない。

**推奨:**
- JSON Schema による入力検証
- 悪意のあるPayloadに対する防御 (サイズ制限、型チェック)

### SQLインジェクション対策

sqlc により完全にパラメータ化されたクエリを使用しているため、SQLインジェクションのリスクは最小限。

### 機密情報の取り扱い

PayloadにはJSON形式で任意のデータを格納可能。機密情報 (パスワード、APIキー等) を含めないよう注意が必要。

**推奨:**
- Payload暗号化
- 機密情報は別ストレージに保存し、IDのみ渡す

## 制限事項と既知の問題

1. **ジョブの重複処理:**
   - GetNextJob() にロック機構がないため、複数ワーカーで同一ジョブを処理する可能性
   - 冪等性のあるプロセッサー設計が必須

2. **SQLiteのスケーラビリティ:**
   - 書き込み処理は単一スレッドに制限される
   - 高負荷環境ではPostgreSQL/MySQL等への移行を推奨

3. **ジョブの優先度逆転:**
   - 長時間実行ジョブが高優先度ジョブをブロックする可能性
   - タイムアウト機構の実装が必要

4. **デッドレターキュー (DLQ) 未実装:**
   - max_retries超過後のジョブは status='failed' で終了
   - 手動での再処理メカニズムが必要

5. **分散トレーシング未対応:**
   - ジョブの処理経路追跡が困難
   - OpenTelemetry等の導入が推奨される

## テスト戦略

### ユニットテスト

- JobQueueService の各メソッド
- 各Processorの Process() メソッド
- ペイロードのシリアライズ/デシリアライズ

### 統合テスト

- Worker のエンドツーエンドジョブ処理
- リトライメカニズムの動作確認
- グレースフルシャットダウンの検証

### 負荷テスト

- 大量ジョブ投入時のスループット測定
- 複数ワーカー並行実行時の競合状態チェック
- データベース接続プール枯渇テスト

## デプロイメント

### 開発環境

```bash
# ワーカー起動
go run cmd/worker/main.go

# 別ターミナルでジョブ投入
go run cmd/worker-manager/main.go enqueue user_created "Test job" 5
```

### 本番環境

```bash
# バイナリビルド
go build -o worker cmd/worker/main.go
go build -o worker-manager cmd/worker-manager/main.go

# systemd サービスとして起動
./worker /var/lib/app/production.db

# ログローテーション
journalctl -u worker -f
```

### Docker化

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o worker cmd/worker/main.go

FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/worker /usr/local/bin/
ENTRYPOINT ["worker"]
CMD ["/data/workers.db"]
```

## まとめ

このWorkerシステムは、シンプルで拡張性の高い非同期ジョブ処理基盤を提供します。SQLiteをバックエンドとすることで依存関係を最小限に抑え、小〜中規模アプリケーションに最適です。

**主要な強み:**
- シンプルなアーキテクチャ
- 型安全なコード生成 (sqlc)
- グレースフルシャットダウン対応
- 柔軟なリトライ戦略

**改善提案:**
- ジョブロック機構の実装
- DLQ (Dead Letter Queue) の追加
- 分散トレーシング対応
- PostgreSQL等へのマイグレーションパス提供