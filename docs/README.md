# minihub 開発・保守資料

この資料は、開発・保守担当が現行実装の構成、データ、処理の流れを追うためのものです。図は Mermaid で記述し、GitHub の Markdown 表示で閲覧できます。仕様の詳細は [DESIGN.md](../DESIGN.md)、利用手順は [利用者マニュアル](user-manual.md)と[管理者マニュアル](admin-manual.md)を参照してください。

| 資料 | 内容 |
| --- | --- |
| [APIリファレンス](api.md) | 認証、curl使用例、エンドポイント、ページング、リアルタイムイベント |
| [アーキテクチャ](architecture.md) | 配置、コンポーネント、認証・認可、ストレージ構成 |
| [データモデル](data-model.md) | 論理ER図、SQLiteの保存構造 |
| [処理フロー](workflows.md) | 投稿、既読、再接続、管理、投票・予定 |
| [AI連携](ai.md) | AIアカウント、送信する履歴、外部APIのHTTP契約 |
| [SQLite保存方式](sqlite.md) | SQLite保存方式、エラー時の復旧、バックアップ |
| [投稿の物理削除](retention-proposal.md) | 削除範囲、停止中の実行手順、復旧 |
| [通信負荷の見積もり](network-load.md) | 500同時接続時の概算、配信・HTTP取得の負荷、導入前の実測 |
| [リリースビルドと手動公開](releases.md) | 4種の配布物、ビルドスクリプト、検証、GitHub Releasesへの登録 |

図を更新するときは `internal/domain`、`internal/service`、`internal/storage`、`internal/httpapi`、`internal/realtime` と照合してください。


## 操作資料

利用者・管理者向けの操作資料です。

| 資料 | 内容 |
| --- | --- |
| [ユーザー向け操作マニュアル](user-manual.md) | チャットの操作、スレッド、リアクション、アンケート、予定調整 |
| [管理者向け操作マニュアル](admin-manual.md) | 初期設定、ユーザー・チャンネル管理、バックアップ、HTTP/HTTPS設定 |
| [ブラウザAPIを使ったOS通知の設定](browser-notifications.md) | ブラウザ通知・OS通知の許可手順とトラブルシューティング |
| [保存方式](sqlite.md) | SQLite保存方式、エラー時の復旧、バックアップ |
| [AIアカウントと外部API連携](ai.md) | AIアカウント、送信する履歴、外部APIのHTTP契約 |
| [投稿の物理削除](retention-proposal.md) | 削除範囲、停止中の実行手順、復旧 |


[検証手順と画面画像の更新](../tests/README.md)も参照してください。

## AI連携

[AIアカウントと外部API連携](ai.md)に設定、送信する履歴、HTTP契約、制限、停止時の動作をまとめています。
