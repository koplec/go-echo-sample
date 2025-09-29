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

## 🔧 ミドルウェアの実装と使用方法

### ミドルウェアとは
ミドルウェアは、HTTPリクエストの処理チェーンに組み込まれる関数で、リクエストやレスポンスを処理する前後に共通の処理を実行できます。

### Echo ミドルウェアの基本構造

```go
// ミドルウェアの基本形
func MyMiddleware() echo.MiddlewareFunc {
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            // リクエスト前の処理
            fmt.Println("Before request")

            // 次のミドルウェアまたはハンドラーを実行
            err := next(c)

            // レスポンス後の処理
            fmt.Println("After request")

            return err
        }
    }
}
```

### 1. バリデーションミドルウェアの実装

**ファイル**: `validator.go`

#### 構造とサービス初期化

```go
type ValidationMiddleware struct {
    router routers.Router // kin-openapi のルーター
}

// ミドルウェアの初期化
func NewValidationMiddleware(specPath string) (*ValidationMiddleware, error) {
    ctx := context.Background()

    // 1. OpenAPIスペックをファイルから読み込み
    loader := &openapi3.Loader{Context: ctx, IsExternalRefsAllowed: true}
    doc, err := loader.LoadFromFile(specPath)
    if err != nil {
        return nil, fmt.Errorf("failed to load OpenAPI spec: %w", err)
    }

    // 2. スペックの妥当性を検証
    if err := doc.Validate(ctx); err != nil {
        return nil, fmt.Errorf("OpenAPI spec validation failed: %w", err)
    }

    // 3. Gorilla Muxルーターを作成（kin-openapiで使用）
    router, err := gorillamux.NewRouter(doc)
    if err != nil {
        return nil, fmt.Errorf("failed to create router: %w", err)
    }

    return &ValidationMiddleware{
        router: router,
    }, nil
}
```

#### ミドルウェア関数の実装

```go
func (v *ValidationMiddleware) Validate() echo.MiddlewareFunc {
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            req := c.Request()

            // 1. リクエストに対応するOpenAPIルートを検索
            route, pathParams, err := v.router.FindRoute(req)
            if err != nil {
                // OpenAPIに定義されていないルートは検証しない
                return next(c)
            }

            // 2. バリデーション用の入力を構築
            requestValidationInput := &openapi3filter.RequestValidationInput{
                Request:    req,
                PathParams: pathParams,
                Route:      route,
            }

            // 3. OpenAPIスキーマに対してリクエストを検証
            ctx := context.Background()
            if err := openapi3filter.ValidateRequest(ctx, requestValidationInput); err != nil {
                return v.handleValidationError(c, err)
            }

            // 4. バリデーション成功時は次の処理に進む
            return next(c)
        }
    }
}
```

#### エラーハンドリングの実装

```go
func (v *ValidationMiddleware) handleValidationError(c echo.Context, err error) error {
    var errorMessage string

    // エラータイプによって適切なメッセージを生成
    switch e := err.(type) {
    case *openapi3filter.RequestError:
        // パラメータエラー
        if e.Parameter != nil {
            errorMessage = fmt.Sprintf("Parameter validation failed for '%s': %s",
                e.Parameter.Name, e.Err.Error())
        }
        // リクエストボディエラー
        else if e.RequestBody != nil {
            errorMessage = fmt.Sprintf("Request body validation failed: %s", e.Err.Error())
        }
        // その他のリクエストエラー
        else {
            errorMessage = fmt.Sprintf("Request validation failed: %s", e.Err.Error())
        }
    case *openapi3filter.SecurityRequirementsError:
        errorMessage = "Security requirements not met"
    default:
        errorMessage = err.Error()
    }

    // エラーメッセージをユーザーフレンドリーに変換
    errorMessage = v.formatErrorMessage(errorMessage)

    return c.JSON(http.StatusBadRequest, map[string]string{
        "error": errorMessage,
    })
}

// エラーメッセージの整形
func (v *ValidationMiddleware) formatErrorMessage(message string) string {
    // より読みやすいメッセージに変換
    message = strings.ReplaceAll(message, "doesn't match schema", "does not match the required format")
    message = strings.ReplaceAll(message, "Error at", "Error in field")
    message = strings.ReplaceAll(message, "Property", "Field")

    if strings.Contains(message, "minimum") {
        message = strings.ReplaceAll(message, "minimum", "must be at least")
    }

    if strings.Contains(message, "format") && strings.Contains(message, "email") {
        message = "Email address format is invalid"
    }

    if strings.Contains(message, "required") {
        message = strings.ReplaceAll(message, "property", "field")
    }

    return message
}
```

#### main-variants.go での使用方法

```go
func createApp(validationMode string) (*echo.Echo, error) {
    e := echo.New()

    // 1. 組み込みミドルウェアの追加
    e.Use(middleware.Logger())   // リクエストログ
    e.Use(middleware.Recover())  // パニックからの回復

    // 2. バリデーション仕様ファイルの選択
    var specFile string
    switch validationMode {
    case "flexible":
        specFile = "openapi-flexible.yaml"
    case "strict":
        specFile = "openapi-strict.yaml"
    default:
        specFile = "openapi.yaml"
    }

    // 3. バリデーションミドルウェアの初期化と登録
    validationMiddleware, err := NewValidationMiddleware(specFile)
    if err != nil {
        return nil, fmt.Errorf("failed to initialize validation middleware: %w", err)
    }

    // 4. ミドルウェアの登録（全ルートに適用）
    e.Use(validationMiddleware.Validate())

    // 5. ルートハンドラーの登録
    // この時点で、すべてのリクエストは上記のミドルウェアチェーンを通る
    e.POST("/users", func(c echo.Context) error {
        return userService.CreateUser(c)
    })

    return e, nil
}
```

### 2. 組み込みミドルウェアの使用

#### Logger ミドルウェア

```go
// 基本的なログミドルウェア
e.Use(middleware.Logger())

// カスタムログ設定
e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
    Format: "${time_rfc3339} ${method} ${uri} ${status} ${latency_human}\n",
    Output: os.Stdout,
}))

// 構造化ログの例
e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
    Skipper: middleware.DefaultSkipper,
    Format: `{"time":"${time_rfc3339_nano}","id":"${id}","remote_ip":"${remote_ip}",` +
        `"host":"${host}","method":"${method}","uri":"${uri}","user_agent":"${user_agent}",` +
        `"status":${status},"error":"${error}","latency":${latency},"latency_human":"${latency_human}",` +
        `"bytes_in":${bytes_in},"bytes_out":${bytes_out}}` + "\n",
}))
```

#### Recover ミドルウェア

```go
// 基本的なリカバリーミドルウェア
e.Use(middleware.Recover())

// カスタムリカバリー設定
e.Use(middleware.RecoverWithConfig(middleware.RecoverConfig{
    Skipper:   middleware.DefaultSkipper,
    StackSize: 1 << 10, // 1KB
    LogErrorFunc: func(c echo.Context, err error, stack []byte) error {
        // カスタムエラーログ
        log.Printf("[PANIC RECOVER] %v %s", err, stack)
        return err
    },
}))
```

#### CORS ミドルウェア

```go
import "github.com/labstack/echo/v4/middleware"

// 基本的なCORS設定
e.Use(middleware.CORS())

// カスタムCORS設定
e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
    AllowOrigins: []string{"https://example.com", "https://app.example.com"},
    AllowMethods: []string{http.MethodGet, http.MethodPut, http.MethodPost, http.MethodDelete},
    AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization},
    AllowCredentials: true,
}))
```

### 3. カスタムミドルウェアの実装例

#### リクエストID ミドルウェア

```go
func RequestIDMiddleware() echo.MiddlewareFunc {
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            // リクエストIDを生成
            requestID := c.Request().Header.Get(echo.HeaderXRequestID)
            if requestID == "" {
                requestID = generateRequestID() // UUIDなどを生成
            }

            // コンテキストに保存
            c.Set("request_id", requestID)

            // レスポンスヘッダーにも設定
            c.Response().Header().Set(echo.HeaderXRequestID, requestID)

            return next(c)
        }
    }
}

// 使用例
e.Use(RequestIDMiddleware())
```

#### 認証ミドルウェア

```go
func AuthMiddleware(secretKey string) echo.MiddlewareFunc {
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            // Authorization ヘッダーをチェック
            auth := c.Request().Header.Get("Authorization")
            if auth == "" {
                return c.JSON(http.StatusUnauthorized, map[string]string{
                    "error": "Authorization header required",
                })
            }

            // Bearer トークンの検証
            if !strings.HasPrefix(auth, "Bearer ") {
                return c.JSON(http.StatusUnauthorized, map[string]string{
                    "error": "Invalid authorization format",
                })
            }

            token := strings.TrimPrefix(auth, "Bearer ")

            // JWTトークンの検証（実装例）
            claims, err := validateJWT(token, secretKey)
            if err != nil {
                return c.JSON(http.StatusUnauthorized, map[string]string{
                    "error": "Invalid token",
                })
            }

            // ユーザー情報をコンテキストに保存
            c.Set("user", claims)

            return next(c)
        }
    }
}

// 特定のルートにのみ適用
protectedGroup := e.Group("/api/protected")
protectedGroup.Use(AuthMiddleware("your-secret-key"))
```

#### レート制限ミドルウェア

```go
import "golang.org/x/time/rate"

func RateLimitMiddleware(requestsPerSecond float64, burst int) echo.MiddlewareFunc {
    limiter := rate.NewLimiter(rate.Limit(requestsPerSecond), burst)

    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            if !limiter.Allow() {
                return c.JSON(http.StatusTooManyRequests, map[string]string{
                    "error": "Rate limit exceeded",
                })
            }
            return next(c)
        }
    }
}

// 使用例: 1秒あたり10リクエスト、バースト20
e.Use(RateLimitMiddleware(10.0, 20))
```

### 4. ミドルウェアの実行順序

```go
func setupMiddlewares(e *echo.Echo) {
    // 1. 最初に実行されるミドルウェア（ログ、リカバリー）
    e.Use(middleware.Logger())
    e.Use(middleware.Recover())

    // 2. セキュリティ関連
    e.Use(middleware.Secure())
    e.Use(middleware.CORS())

    // 3. リクエスト前処理
    e.Use(RequestIDMiddleware())
    e.Use(RateLimitMiddleware(10.0, 20))

    // 4. 認証（必要な場合）
    // e.Use(AuthMiddleware("secret"))

    // 5. バリデーション（最後の方で実行）
    validationMiddleware, _ := NewValidationMiddleware("openapi.yaml")
    e.Use(validationMiddleware.Validate())
}
```

### 5. ミドルウェアのテスト

```go
func TestValidationMiddleware(t *testing.T) {
    // Echo インスタンスを作成
    e := echo.New()

    // ミドルウェアを設定
    validationMiddleware, err := NewValidationMiddleware("openapi.yaml")
    assert.NoError(t, err)
    e.Use(validationMiddleware.Validate())

    // テストハンドラーを設定
    e.POST("/users", func(c echo.Context) error {
        return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
    })

    // 正常なリクエストのテスト
    t.Run("Valid Request", func(t *testing.T) {
        req := httptest.NewRequest(http.MethodPost, "/users",
            strings.NewReader(`{"email":"test@example.com","age":25}`))
        req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
        rec := httptest.NewRecorder()

        e.ServeHTTP(rec, req)

        assert.Equal(t, http.StatusOK, rec.Code)
    })

    // 無効なリクエストのテスト
    t.Run("Invalid Request", func(t *testing.T) {
        req := httptest.NewRequest(http.MethodPost, "/users",
            strings.NewReader(`{"age":25}`)) // email が不足
        req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
        rec := httptest.NewRecorder()

        e.ServeHTTP(rec, req)

        assert.Equal(t, http.StatusBadRequest, rec.Code)
        assert.Contains(t, rec.Body.String(), "email")
    })
}
```

### 6. ミドルウェアのベストプラクティス

#### エラーハンドリング

```go
func SafeMiddleware() echo.MiddlewareFunc {
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            defer func() {
                if r := recover(); r != nil {
                    // パニックをキャッチしてログに記録
                    log.Printf("Middleware panic: %v", r)

                    // 適切なエラーレスポンスを返す
                    if !c.Response().Committed {
                        c.JSON(http.StatusInternalServerError, map[string]string{
                            "error": "Internal server error",
                        })
                    }
                }
            }()

            return next(c)
        }
    }
}
```

#### パフォーマンス考慮

```go
func EfficientMiddleware() echo.MiddlewareFunc {
    // 初期化時に重い処理を実行
    heavyResource := initializeHeavyResource()

    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            // リクエストごとには軽い処理のみ
            if shouldSkip(c) {
                return next(c)
            }

            // 必要最小限の処理
            doLightWork(c, heavyResource)

            return next(c)
        }
    }
}
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