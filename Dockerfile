# ==============================================================================
# minihub Dockerfile
#
# 人・AI・外部システムをつなぐ、シンプルなプライベート・メッセージングハブ
#
# 【ビルド方法】
#   docker build -t minihub .
#
# 【ローカル永続化（SQLite）での起動方法】
#   1. ホストマシンのローカルディレクトリへマウント（推奨）:
#      - Linux / macOS (bash / zsh):
#          mkdir -p data
#          docker run -d \
#            --name minihub \
#            -p 8080:8080 \
#            -v "$(pwd)/data:/app/data" \
#            -v "$(pwd)/minihub.json:/app/minihub.json" \
#            minihub
#
#      - Windows (PowerShell):
#          New-Item -ItemType Directory -Force -Path data
#          docker run -d `
#            --name minihub `
#            -p 8080:8080 `
#            -v "${PWD}/data:/app/data" `
#            -v "${PWD}/minihub.json:/app/minihub.json" `
#            minihub
#
#   2. Docker 名前付きボリュームでの永続化:
#      docker volume create minihub-data
#      docker run -d \
#        --name minihub \
#        -p 8080:8080 \
#        -v minihub-data:/app/data \
#        minihub
#
# 【永続化マッピングの注意点】
#   - minihub は組み込みSQLite（WALモード）とファイルロック（.minihub.lock）を使用します。
#   - データ領域 (/app/data) には以下のファイルが生成されます：
#       - minuhub.db     : SQLite メインデータベース
#       - minuhub.db-wal : WAL（先行書き込みログ）
#       - minuhub.db-shm : WAL共有メモリ
#       - storage.json   : 保存方式マーカー
#       - .minihub.lock  : プロセス多重起動防止ロック
#       - server.log     : サーバーログファイル
#   - NFS/SMB等のネットワーク共有や同期ツール管理下（OneDrive/Dropbox等）のフォルダを
#     マウントすると、SQLiteロック競合や破損の原因となるため、必ずローカルSSD/HDD上の
#     パスをマウントしてください。
#   - 設定ファイル（minihub.json）をマウントする場合は、minihub.sample.json をコピーして
#     initialAdmin.password などを設定したものをマウントしてください。
#
# 【無停止バックアップ】
#   稼働中のコンテナから安全にSQLiteスナップショットを取得できます：
#   docker exec minihub /app/minihub backup -out /app/data/backup.db
# ==============================================================================

# ------------------------------------------------------------------------------
# 1. ビルドステージ (Go ツールチェーン)
# ------------------------------------------------------------------------------
FROM golang:alpine AS builder

WORKDIR /src

# 依存モジュールのキャッシュ
COPY go.mod go.sum ./
RUN go mod download

# ソースコードのコピーと静的バイナリビルド (CGO不要)
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /bin/minihub \
    ./cmd/minihub

# ------------------------------------------------------------------------------
# 2. 実行用最小ステージ (Alpine Linux)
# ------------------------------------------------------------------------------
FROM alpine:3.21

# AI連携（OpenAI互換APIへのHTTPS通信）に必要なルート証明書と、タイムゾーンデータを導入
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# ビルドした実行バイナリと初期設定サンプルをコピー
COPY --from=builder /bin/minihub /app/minihub
COPY minihub.sample.json /app/minihub.sample.json

# SQLiteデータベース・ログ保存先ディレクトリ
# ホスト側の永続化ディレクトリをここにマウントしてください
VOLUME ["/app/data"]

# Web UI / WebSocket / Webhook 用の受付ポート
EXPOSE 8080

# コンテナ起動時に 0.0.0.0 で待ち受けるよう指定
ENTRYPOINT ["/app/minihub"]
CMD ["-addr", "0.0.0.0:8080", "-data", "/app/data"]

