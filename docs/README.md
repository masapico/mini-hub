# minihub 開発・保守資料

この資料は、人・AI・外部システムをつなぐプライベート・メッセージハブ `minihub` の開発・保守担当が現行実装の構成、データ、処理の流れを追うためのものです。図は Mermaid で記述し、GitHub の Markdown 表示で閲覧できます。仕様の詳細は [DESIGN.md](../DESIGN.md)、利用手順は [ユーザー向け操作マニュアル](user-manual.md)と[管理者向け操作マニュアル](admin-manual.md)を参照してください。

| 資料 | 内容 |
| --- | --- |
| [APIリファレンス](api.md) | 認証、エンドポイント、Incoming Webhook API、リアルタイムイベント |
| [アーキテクチャ](architecture.md) | メッセージハブの配置、コンポーネント、Webhook・AI連携、ストレージ構成 |
| [データモデル](data-model.md) | 論理ER図、SQLite（スキーマv8）の保存構造 |
| [処理フロー](workflows.md) | 投稿・既読、Incoming Webhook受信、AIメンション、再接続、管理、投票・予定 |
| [AI連携](ai.md) | AIアカウント、送信する履歴、添付ファイル処理、外部APIのHTTP契約 |
| [SQLite保存方式](sqlite.md) | SQLite保存方式、エラー時の復旧、無停止バックアップ |
| [投稿の物理削除](retention-proposal.md) | 削除範囲、停止中の実行手順、復旧 |
| [通信負荷の見積もり](network-load.md) | 500同時接続時の概算、配信・HTTP取得の負荷、導入前の実測 |
| [リリースビルドと手動公開](releases.md) | 4種の配布物、ビルドスクリプト、検証、GitHub Releasesへの登録 |

図を更新するときは `internal/domain`、`internal/service`、`internal/storage`、`internal/httpapi`、`internal/realtime` と照合してください。


## 操作資料

利用者・管理者向けの操作資料です。

| 資料 | 内容 |
| --- | --- |
| [ユーザー向け操作マニュアル](user-manual.md) | 基本操作、スレッド、リアクション、アンケート、予定調整、AI呼び出し・AIキャンバス |
| [管理者向け操作マニュアル](admin-manual.md) | 初期設定、ユーザー・チャンネル・グループ管理、AI設定、Webhook設定、バックアップ、HTTP/HTTPS |
| [ブラウザAPIを使ったOS通知の設定](browser-notifications.md) | ブラウザ通知・OS通知の許可手順とトラブルシューティング |
| [保存方式](sqlite.md) | SQLite保存方式、エラー時の復旧、バックアップ |
| [AIアカウントと外部API連携](ai.md) | AIアカウント、送信する履歴、外部APIのHTTP契約 |
| [投稿の物理削除](retention-proposal.md) | 削除範囲、停止中の実行手順、復旧 |


[検証手順と画面画像の更新](../tests/README.md)も参照してください。
