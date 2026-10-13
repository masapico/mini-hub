# アーキテクチャ

[設計図の目次](README.md) / [データモデル](data-model.md) / [処理フロー](workflows.md)

## 配置

```mermaid
flowchart LR
    U["利用者・管理者のブラウザ"]
    subgraph HOST["イントラネット内の稼働マシン"]
        APP["minihub 単一Goプロセス<br/>静的Web UI / HTTP API / WebSocket"]
        DIR["ローカル dataDir<br/>排他ロック・storage.json"]
        DB["SQLite方式<br/>minuhub.db / WAL"]
        APP --> DIR
        DIR --> DB
    end
    U <-->|"HTTP / WebSocket"| APP
```

永続化は組み込みSQLiteを使用します。外部DBサーバーはありません。データディレクトリのプロセス排他ロック（`.minihub.lock`）を取得し、安全に動作します。旧バージョンのfile方式データが存在する場合は、`minihub migrate -to sqlite` で移行します。

## コンポーネントと責務

```mermaid
flowchart LR
    UI["web<br/>埋め込み静的画面"]
    API["httpapi<br/>HTTPルーティング・CSRF確認"]
    AUTH["auth<br/>Cookieセッション"]
    SVC["service<br/>権限・業務ルール"]
    HUB["realtime<br/>接続・配信・在席"]
    STORE["storage.Storage<br/>永続化契約"]
    BACK["storage/backend<br/>SQLiteオープン・排他ロック・移行"]
    SQL["sqlitestore"]
    MODEL["domain<br/>保存モデル"]

    UI --> API
    UI <-->|"WebSocket"| HUB
    API --> AUTH
    API --> SVC
    HUB --> AUTH
    HUB -->|"閲覧・投稿権限を照会"| SVC
    SVC --> STORE
    SVC -->|"コミット後に通知"| HUB
    BACK --> SQL
    SQL -.->|"実装"| STORE
    STORE --> MODEL
```

`cmd/minihub` が設定を読み、ストレージ・初期管理者・サービス・HTTP・WebSocket を組み立てます。HTTP更新要求はセッション認証とCSRF確認を経て、サービス層で権限を判定します。非公開チャンネルの閲覧・投稿可否はサーバー側で判定し、WebSocket配信時にも閲覧権限を確認します。

保存処理が成功した後に、WebSocketはチャンネルIDや連番などの軽量イベントを配信します。クライアントは必要な本文や集計をHTTP APIから取得します。通常終了ではHTTP受付を止め、WebSocket接続を閉じてからストレージを閉じます。

## ストレージの書き込みと復旧

```mermaid
flowchart TB
    A["Storage.AddMessage"] --> S1["単一書き込み接続・短いトランザクション"]
    S1 --> S2["countersの採番値とmessagesを同時コミット"]
    S2 --> C["コミット済みメッセージを返す"]
```

SQLite方式は永続索引を使い、採番値を `counters` テーブルに安全に保持します。WALモードにより読み書きが分離され、高速かつ堅牢に動作します。


## フロントエンド依存

Web画面、Bootstrap、Bootstrap Icons、faviconはGoの実行ファイルへ埋め込まれています。ビルド後の別途配置やCDN接続は不要です。

- Bootstrap 5.3.8
- Bootstrap Icons 1.13.1

配布用のCSS、JavaScript、SVG spriteとライセンスを `web/vendor/` に同梱しています。実行時にnpmやインターネット接続は使用しません。
