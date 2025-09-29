# 開発者向け解説：Go Echo + バックグラウンドワーカー システム

## 📖 概要

このドキュメントは、GoのEchoフレームワークを使用したWebサーバーと、バックグラウンドワーカーシステムの実装について詳細に解説します。実際の開発現場で同様のシステムを構築する際の参考資料として作成されています。

## 🏗️ システム全体アーキテクチャ

### 基本構成
```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   Web Server    │───▶│   Job Queue      │───▶│  Worker Process │
│   (Echo)        │    │   (SQLite DB)    │    │  (Background)   │
└─────────────────┘    └──────────────────┘    └─────────────────┘
         │                        │                        │
         ▼                        ▼                        ▼
    HTTP Request            Job Storage              Async Processing
    JSON Validation         Priority/Retry          Email/Analytics
    User Creation           Status Tracking         Data Processing
```

### データフロー
1. **HTTP Request** → Webサーバーが受信
2. **JSON Validation** → OpenAPIスキーマでバリデーション
3. **Database Storage** → ユーザーデータを保存
4. **Job Enqueue** → バックグラウンドジョブをキューに追加
5. **Worker Processing** → 別プロセスで非同期処理
6. **Job Completion** → ジョブ完了/失敗の記録

## 📁 ファイル構成と役割

### コアファイル

#### `main-variants.go` - Webサーバーのエントリーポイント
```go
// 主な責務：
// - Echoサーバーの起動
// - ルーティング設定
// - ミドルウェア設定（バリデーション、ロギング）
// - 環境変数による動作モード切り替え
```

#### `database.go` - データベースサービス層
```go
// 主な責務：
// - SQLite接続管理
// - スキーマ初期化
// - sqlc生成コードのラッパー
// - ジョブキューサービスの統合
// - ユーザー作成時の自動ジョブエンキュー
```

#### `job-queue.go` - ジョブキューサービス
```go
// 主な責務：
// - ジョブのエンキュー/デキュー
// - ジョブステータス管理
// - リトライ機能
// - 優先度管理
// - ジョブ統計情報の取得
```

#### `worker.go` - バックグラウンドワーカー
```go
// 主な責務：
// - ジョブの並行処理
// - プロセッサーによる処理分岐
// - グレースフルシャットダウン
// - エラーハンドリングとリトライ
// - ワーカープールの管理
```

#### `validator.go` - バリデーションミドルウェア
```go
// 主な責務：
// - OpenAPIスキーマの読み込み
// - リクエストボディのバリデーション
// - エラーメッセージのフォーマット
// - 複数バリデーションモードの対応
```

## 🔧 技術スタック詳細

### 1. OpenAPI + oapi-codegen

**目的**: API仕様駆動開発とコード自動生成

**仕組み**:
```yaml
# openapi.yaml
components:
  schemas:
    UserRequest:
      type: object
      required: [email, age]
      additionalProperties: false  # 厳格モード
      properties:
        email:
          type: string
          format: email
        age:
          type: integer
          minimum: 0
```

**自動生成されるコード**:
```go
// generated/types.go
type UserRequest struct {
    Email openapi_types.Email `json:"email"`
    Age   int                 `json:"age"`
    // オプショナルフィールド
    Name     *string `json:"name,omitempty"`
    Bio      *string `json:"bio,omitempty"`
    IsActive *bool   `json:"is_active,omitempty"`
}
```

**メリット**:
- スキーマファーストの開発
- 型安全性の保証
- ドキュメントとコードの同期
- バリデーションロジックの自動化

### 2. kin-openapi バリデーション

**目的**: リアルタイムリクエストバリデーション

**実装例**:
```go
// validator.go
func (v *ValidationMiddleware) Validate() echo.MiddlewareFunc {
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            req := c.Request()

            // ルート検索
            route, pathParams, err := v.router.FindRoute(req)
            if err != nil {
                return next(c) // バリデーション対象外
            }

            // バリデーション実行
            requestValidationInput := &openapi3filter.RequestValidationInput{
                Request:    req,
                PathParams: pathParams,
                Route:      route,
            }

            if err := openapi3filter.ValidateRequest(ctx, requestValidationInput); err != nil {
                return v.handleValidationError(c, err)
            }

            return next(c)
        }
    }
}
```

**バリデーション戦略**:
1. **デフォルトモード**: 定義済みフィールドのみ許可
2. **フレキシブルモード**: `additionalProperties: true`
3. **厳格モード**: `additionalProperties: false` + 厳密チェック

### 3. sqlc による型安全SQL

**目的**: SQLクエリのタイプセーフティ

**定義**:
```sql
-- queries.sql
-- name: CreateUser :one
INSERT INTO users (email, age, name, bio, is_active, additional_data)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetNextPendingJob :one
SELECT * FROM job_queue
WHERE status = 'pending'
  AND scheduled_at <= CURRENT_TIMESTAMP
  AND retry_count < max_retries
ORDER BY priority DESC, scheduled_at ASC
LIMIT 1;
```

**自動生成されるコード**:
```go
// db/queries.sql.go
func (q *Queries) CreateUser(ctx context.Context, arg CreateUserParams) (User, error) {
    row := q.db.QueryRowContext(ctx, createUser,
        arg.Email,
        arg.Age,
        arg.Name,
        arg.Bio,
        arg.IsActive,
        arg.AdditionalData,
    )
    var i User
    err := row.Scan(&i.ID, &i.Email, &i.Age, /* ... */)
    return i, err
}
```

**メリット**:
- SQLインジェクション対策
- コンパイル時の型チェック
- パフォーマンスの最適化
- SQLとGoコードの分離

## 🔄 ジョブキューシステム詳細

### ジョブライフサイクル

```
[PENDING] ──┐
            ├──▶ [PROCESSING] ──┐
            │                   ├──▶ [COMPLETED]
            │                   └──▶ [FAILED] ──┐
            │                                   │
            └──◀── [RETRY_PENDING] ◀────────────┘
```

### データベーススキーマ

```sql
CREATE TABLE job_queue (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_type TEXT NOT NULL,           -- 'user_created', 'data_analysis', etc.
    payload TEXT NOT NULL,            -- JSON形式のペイロード
    status TEXT NOT NULL DEFAULT 'pending', -- ジョブステータス
    priority INTEGER DEFAULT 0,      -- 優先度 (高い数値 = 高優先度)
    max_retries INTEGER DEFAULT 3,   -- 最大リトライ回数
    retry_count INTEGER DEFAULT 0,   -- 現在のリトライ回数
    error_message TEXT,              -- エラーメッセージ
    scheduled_at DATETIME DEFAULT CURRENT_TIMESTAMP, -- 実行予定時刻
    started_at DATETIME,             -- 処理開始時刻
    completed_at DATETIME,           -- 処理完了時刻
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### ジョブエンキューの実装

```go
// database.go - ユーザー作成時の自動エンキュー
func (ds *DatabaseService) CreateUser(userReq generated.UserRequest, additionalProps map[string]interface{}) (*generated.User, error) {
    // 1. ユーザーデータをDBに保存
    user, err := ds.convertDBUserToGenerated(dbUser)
    if err != nil {
        return nil, err
    }

    // 2. ジョブペイロードを構築
    jobPayload := JobPayload{
        UserID:          &user.Id,
        UserData:        map[string]interface{}{
            "id":        user.Id,
            "email":     user.Email,
            "age":       user.Age,
            "name":      user.Name,
            "bio":       user.Bio,
            "is_active": user.IsActive,
        },
        AdditionalProps: additionalProps, // 追加プロパティも保存
    }

    // 3. バックグラウンドジョブをエンキュー
    _, jobErr := ds.jobQueue.EnqueueJob(JobUserCreated, jobPayload, 1)
    if jobErr != nil {
        // ジョブエンキューエラーはログに記録するが、ユーザー作成は失敗させない
        fmt.Printf("Failed to enqueue job for user %d: %v\n", user.Id, jobErr)
    }

    return user, nil
}
```

### ワーカーによるジョブ処理

```go
// worker.go - ジョブ処理の実装
func (w *Worker) processNextJob(processors map[JobType]JobProcessor) {
    // 1. 次の処理対象ジョブを取得
    job, err := w.jobQueue.GetNextJob()
    if err != nil {
        log.Printf("Error getting next job: %v", err)
        return
    }

    if job == nil {
        return // ジョブなし
    }

    // 2. ペイロードをパース
    var payload JobPayload
    if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
        w.jobQueue.FailJob(job.ID, fmt.Sprintf("Failed to parse payload: %v", err), false)
        return
    }

    // 3. 適切なプロセッサーを選択
    processor, exists := processors[JobType(job.JobType)]
    if !exists {
        w.jobQueue.FailJob(job.ID, fmt.Sprintf("No processor for job type: %s", job.JobType), false)
        return
    }

    // 4. ジョブを実行
    if err := processor.Process(job, payload); err != nil {
        // エラー時はリトライするか失敗にする
        shouldRetry := job.RetryCount < job.MaxRetries
        w.jobQueue.FailJob(job.ID, err.Error(), shouldRetry)
    } else {
        // 成功時は完了マーク
        w.jobQueue.CompleteJob(job.ID)
    }
}
```

## 🎯 プロセッサーパターン

### インターフェース定義

```go
type JobProcessor interface {
    Process(job *db.JobQueue, payload JobPayload) error
    JobType() JobType
}
```

### 具体的な実装例

```go
// UserCreatedProcessor - ユーザー作成後処理
type UserCreatedProcessor struct{}

func (p *UserCreatedProcessor) JobType() JobType {
    return JobUserCreated
}

func (p *UserCreatedProcessor) Process(job *db.JobQueue, payload JobPayload) error {
    log.Printf("Processing user created job %d for user %d", job.ID, *payload.UserID)

    // 実際の処理内容
    // 1. ウェルカムメール送信
    fmt.Printf("📧 Sending welcome email to user %d (%s)\n", *payload.UserID, payload.UserData["email"])

    // 2. 追加プロパティの分析
    if len(payload.AdditionalProps) > 0 {
        fmt.Printf("🔍 Analyzing additional user properties: %v\n", payload.AdditionalProps)

        for key, value := range payload.AdditionalProps {
            switch key {
            case "hobby":
                fmt.Printf("   - User's hobby: %v\n", value)
                // 趣味に基づくレコメンデーション設定
            case "location":
                fmt.Printf("   - User's location: %v\n", value)
                // 地域別サービス設定
            case "score":
                fmt.Printf("   - User's score: %v\n", value)
                // スコアに基づくランク設定
            }
        }
    }

    // 3. アナリティクス記録
    fmt.Printf("📊 Recording user signup metrics for user %d\n", *payload.UserID)

    // 4. ユーザープロファイル初期化
    fmt.Printf("⚙️  Setting up user profile for user %d\n", *payload.UserID)

    // 処理時間をシミュレート
    time.Sleep(time.Millisecond * 500)

    return nil
}
```

## 🛠️ 開発時のベストプラクティス

### 1. エラーハンドリング

```go
// 適切なエラーハンドリングの例
func (jq *JobQueueService) EnqueueJob(jobType JobType, payload JobPayload, priority int) (*db.JobQueue, error) {
    // 1. ペイロードの検証
    payloadJSON, err := json.Marshal(payload)
    if err != nil {
        return nil, fmt.Errorf("failed to marshal payload: %w", err)
    }

    // 2. データベース操作
    job, err := jq.queries.CreateJob(context.Background(), db.CreateJobParams{
        JobType:     string(jobType),
        Payload:     string(payloadJSON),
        Priority:    int64(priority),
        MaxRetries:  3,
        ScheduledAt: time.Now(),
    })
    if err != nil {
        return nil, fmt.Errorf("failed to create job: %w", err)
    }

    return &job, nil
}
```

### 2. コンテキスト管理

```go
// コンテキストを適切に伝播する例
func (ds *DatabaseService) CreateUser(ctx context.Context, userReq generated.UserRequest) (*generated.User, error) {
    // データベース操作時にコンテキストを渡す
    dbUser, err := ds.queries.CreateUser(ctx, db.CreateUserParams{
        Email:          string(userReq.Email),
        Age:            int64(userReq.Age),
        // ... その他のパラメータ
    })
    if err != nil {
        return nil, fmt.Errorf("failed to create user: %w", err)
    }

    // ジョブエンキュー時もコンテキストを考慮
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
        // ジョブエンキュー処理
    }

    return user, nil
}
```

### 3. 設定の外部化

```go
// 設定値を環境変数で管理
type Config struct {
    Port        string
    DatabaseURL string
    WorkerCount int
    LogLevel    string
}

func LoadConfig() *Config {
    return &Config{
        Port:        getEnvOrDefault("PORT", "8080"),
        DatabaseURL: getEnvOrDefault("DATABASE_URL", "users.db"),
        WorkerCount: getEnvIntOrDefault("WORKER_COUNT", 3),
        LogLevel:    getEnvOrDefault("LOG_LEVEL", "info"),
    }
}

func getEnvOrDefault(key, defaultValue string) string {
    if value := os.Getenv(key); value != "" {
        return value
    }
    return defaultValue
}
```

## 🔍 デバッグとモニタリング

### ログの構造化

```go
// 構造化ログの例
import "github.com/sirupsen/logrus"

func (w *Worker) processJob(job *db.JobQueue) {
    logger := logrus.WithFields(logrus.Fields{
        "worker_id": w.id,
        "job_id":    job.ID,
        "job_type":  job.JobType,
    })

    logger.Info("Starting job processing")

    startTime := time.Now()
    defer func() {
        logger.WithField("duration", time.Since(startTime)).Info("Job processing completed")
    }()

    // ジョブ処理ロジック
}
```

### メトリクス収集

```go
// Prometheusメトリクスの例
var (
    jobsProcessed = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "jobs_processed_total",
            Help: "Total number of processed jobs",
        },
        []string{"job_type", "status"},
    )

    jobDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "job_duration_seconds",
            Help: "Duration of job processing",
        },
        []string{"job_type"},
    )
)

func (p *UserCreatedProcessor) Process(job *db.JobQueue, payload JobPayload) error {
    timer := prometheus.NewTimer(jobDuration.WithLabelValues(string(p.JobType())))
    defer timer.ObserveDuration()

    defer func() {
        status := "success"
        if err != nil {
            status = "error"
        }
        jobsProcessed.WithLabelValues(string(p.JobType()), status).Inc()
    }()

    // 処理ロジック
    return nil
}
```

## 🚀 デプロイメント考慮事項

### 1. Graceful Shutdown

```go
// シグナルハンドリングによる優雅な停止
func main() {
    // サーバー起動
    server := &http.Server{Addr: ":8080", Handler: e}

    // Graceful shutdown
    go func() {
        sigCh := make(chan os.Signal, 1)
        signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
        <-sigCh

        log.Println("Shutdown signal received")

        // 30秒でタイムアウト
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()

        if err := server.Shutdown(ctx); err != nil {
            log.Printf("Server shutdown error: %v", err)
        }
    }()

    if err := server.ListenAndServe(); err != http.ErrServerClosed {
        log.Fatalf("Server failed: %v", err)
    }
}
```

### 2. ヘルスチェックエンドポイント

```go
// ヘルスチェックの実装
func (s *Server) healthCheck(c echo.Context) error {
    // データベース接続確認
    if err := s.db.Ping(); err != nil {
        return c.JSON(http.StatusServiceUnavailable, map[string]string{
            "status": "unhealthy",
            "error":  "database connection failed",
        })
    }

    // ワーカーの状態確認
    stats, err := s.jobQueue.GetJobStats()
    if err != nil {
        return c.JSON(http.StatusServiceUnavailable, map[string]string{
            "status": "unhealthy",
            "error":  "job queue unavailable",
        })
    }

    return c.JSON(http.StatusOK, map[string]interface{}{
        "status": "healthy",
        "database": "connected",
        "jobs": map[string]interface{}{
            "pending":    stats.PendingCount,
            "processing": stats.ProcessingCount,
            "failed":     stats.FailedCount,
        },
    })
}
```

## 📚 参考資料

### 使用ライブラリ

1. **Echo Framework**: https://echo.labstack.com/
   - 高パフォーマンスなHTTPルーター
   - ミドルウェアサポート
   - コンテキスト管理

2. **sqlc**: https://sqlc.dev/
   - SQLからGoコード生成
   - 型安全なクエリ実行
   - PostgreSQL、MySQL、SQLite対応

3. **kin-openapi**: https://github.com/getkin/kin-openapi
   - OpenAPI 3.0仕様の処理
   - リクエスト/レスポンスバリデーション
   - スキーマ検証

4. **oapi-codegen**: https://github.com/deepmap/oapi-codegen
   - OpenAPIからGoコード生成
   - クライアント/サーバーコード対応
   - 複数のHTTPルーター対応

### 設計パターン

1. **Repository Pattern**: データアクセス層の抽象化
2. **Worker Pool Pattern**: 並行ジョブ処理
3. **Publisher-Subscriber Pattern**: ジョブキューシステム
4. **Middleware Pattern**: 横断的関心事の処理
5. **Strategy Pattern**: 複数バリデーションモード

## 🎯 明日の作業に向けて

### チェックリスト

- [ ] OpenAPIスキーマの設計と定義
- [ ] データベーススキーマの設計
- [ ] ジョブタイプの定義と実装
- [ ] エラーハンドリング戦略の確立
- [ ] ロギングとモニタリングの設定
- [ ] テスト戦略の策定
- [ ] デプロイメント方法の検討

### 推奨する開発順序

1. **基本のWebサーバー構築**
2. **OpenAPIスキーマ定義**
3. **データベース設計と実装**
4. **バリデーション機能実装**
5. **ジョブキューシステム構築**
6. **ワーカープロセス実装**
7. **テストとデバッグ**
8. **監視とログ設定**

このガイドが明日からの開発作業の参考になることを願っています！