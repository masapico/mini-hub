# minihub × AI連携・添付ファイル仕様・インターフェース規約書

本ドキュメントは、プライベート・メッセージハブ **minihub** において、ユーザーがAIメンション時にファイルを添付した場合、OpenAI互換リクエスト対応のAIサービスに対してどのような形式でデータを渡すかを定義した連携インターフェース規約書です。

---

## 1. 基本方針

1. **OpenAI Chat Completions 互換プロトコルの Content Parts 方式（Base64）**
   - 添付ファイルは、OpenAI互換 API のメッセージ本文（`content`）における Content Parts 配列形式（`type: "file"`、Base64データ）として直接リクエストペイロード内に含めて送信します。
2. **minihub ホスト内のファイル非保持（インメモリ・ステートレス）**
   - 添付ファイルは minihub サーバーのディスクストレージには一切保存されません。メモリ上で一時ステージングされ、投稿保存時に Base64 化されて AI 連携キューに渡された後、即座にメモリから解放・破棄されます。
   - これにより、サーバーディスク容量の圧迫や起動時・定期的なファイル削除バッチが不要となり、運用が極めて簡潔になります。
3. **基準日時（サーバーローカルタイム）の送信時付与**
   - LLM に現在日時を把握させるため、トリガーメッセージ（呼び出し投稿）のユーザー発言テキスト先頭にサーバーのローカルタイム（例: `[現在日時: 2026-10-13 15:00:00 JST]\n`）が送信時のみ自動付与されます（データベース内のメッセージ本文には保存されません）。
4. **スレッド過去メッセージの添付ファイルの軽量化**
   - スレッド内の過去メッセージに添付があった場合、コンテキスト肥大化およびトークン浪費を防ぐため、過去メッセージの Base64 データは再送しません。テキスト末尾に `[添付ファイル: <ファイル名>]` のタグを付与して送信します。

---

## 2. HTTP リクエスト仕様

### 2.1 リクエストヘッダー
| ヘッダー名 | 必須 | 内容・例 |
| :--- | :---: | :--- |
| `Content-Type` | 必須 | `application/json` |
| `Accept` | 必須 | `application/json` |
| `X-MiniHub-Request-ID` | 必須 | minihub の一意な依頼ID（UUID等、ログ追跡用） |
| `Authorization` | 任意 | `minihub.json` にトークンが設定されている場合、`Bearer <token>` |

### 2.2 リクエストサイズ上限
- **添付ファイルあり**: 最大 **70 MiB**（Base64 エンコードに伴う容量増を許容）
- **添付ファイルなし**: 最大 **1 MiB**

### 2.3 エンドポイント
- `POST {AI_SERVICE_BASE_URL}/v1/chat/completions`

---

## 3. メッセージ構造仕様（Content Parts）

添付ファイルがある呼び出し投稿（トリガーメッセージ）の `messages` 内の `content` は、文字列ではなく以下の要素オブジェクトの配列（Content Parts）になります。

### 3.1 テキストパート (`type: "text"`)
- `type`: `"text"`
- `text`: 発言者名、基準日時、ユーザー本文を含むテキスト。

### 3.2 ファイルパート (`type: "file"`)
- `type`: `"file"`
- `name`: アップロード時のファイル名（例: `sales_report.xlsx`）
- `data`: ファイル内容全体の Base64 エンコード文字列

---

## 4. 対応ファイル形式

添付可能なファイル拡張子（最大5件、1ファイル20MB・合計50MB以内）：
- **Microsoft Office**:
  - Excel（`.xlsx`, `.xls`）
  - Word（`.docx`, `.doc`）
  - PowerPoint（`.pptx`, `.ppt`）
- **ドキュメント**:
  - PDF（`.pdf`）
- **プレーンテキスト / データ**:
  - `.txt`, `.csv`, `.tsv`, `.json`, `.md`

---

## 5. リクエスト JSON の具体例

### 例1: 単一の Excel ファイルが添付された場合
```json
{
  "model": "local-model",
  "stream": false,
  "messages": [
    {
      "role": "system",
      "content": "社内アシスタントとして日本語で簡潔に回答してください。"
    },
    {
      "role": "user",
      "content": [
        {
          "type": "text",
          "text": "山田 太郎 (yamada_t):\n[現在日時: 2026-10-13 15:00:00 JST]\n@ai:assistant この売上データを分析して、先月比の成長要因を箇条書きで教えてください。"
        },
        {
          "type": "file",
          "name": "sales_report.xlsx",
          "data": "UEsDBBQACAgIAI1pSVkAAAAAAAAAAAAAAA..."
        }
      ]
    }
  ]
}
```

### 例2: スレッド履歴＋複数ファイルが添付された場合
（スレッド過去メッセージの添付はテキスト末尾にファイル名のみ、最新のトリガーメッセージは Base64 の Content Parts）
```json
{
  "model": "local-model",
  "stream": false,
  "messages": [
    {
      "role": "system",
      "content": "社内アシスタントとして日本語で簡潔に回答してください。"
    },
    {
      "role": "user",
      "content": "山田 太郎 (yamada_t):\n@ai:assistant 営業提案書のレビューをお願いします。\n[添付ファイル: proposal_draft.docx]"
    },
    {
      "role": "assistant",
      "content": "承知いたしました。提案書と追加資料をお知らせください。"
    },
    {
      "role": "user",
      "content": [
        {
          "type": "text",
          "text": "佐藤 花子 (sato_h):\n[現在日時: 2026-10-13 15:05:00 JST]\n@ai:assistant 以下の2点を添付します。確認してください。"
        },
        {
          "type": "file",
          "name": "proposal_v2.docx",
          "data": "UEsDBBQACAgI..."
        },
        {
          "type": "file",
          "name": "cost_estimate.pdf",
          "data": "JVBERi0xLjQK..."
        }
      ]
    }
  ]
}
```

---

## 6. AI サービス側のレスポンス仕様

### 6.1 レスポンス形式 (200 OK)
minihub の標準仕様に準拠した形式で返却します。
```json
{
  "id": "chatcmpl-xxxxxxxxxxxx",
  "object": "chat.completion",
  "created": 1728800000,
  "model": "local-model",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "添付いただいた資料を確認しました。以下の改善点を提案します。\n1. ..."
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 1250,
    "completion_tokens": 340,
    "total_tokens": 1590
  }
}
```

### 6.2 回答サイズ上限
- 回答本文は UTF-8 で **16 KiB** 以内です。

---

## 7. minihub 実装仕様の要約

1. **ホスト内ファイル完全非保持**:
   minihub ホストのローカルディスクに添付ファイルは保存されません。
2. **ステートレスな Base64 連携**:
   アップロード受領時にメモリ上で Base64 化し、キュー経由で OpenAI 互換 API への POST 完了後に破棄されます。
3. **タイムアウト設定**:
   大容量ファイルの Base64 転送や推論を考慮し、`minihub.json` の `timeout`（既定 `120s`）を適切に設定してください。
