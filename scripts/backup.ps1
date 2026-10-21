<#
.SYNOPSIS
    minihub SQLite データベースの整合性ホットバックアップ & S3 アップロードスクリプト
.DESCRIPTION
    本スクリプトは、稼働中の minihub サーバーを停止することなく、
    SQLite の VACUUM INTO 機能を呼び出してトランザクション整合性のある
    データベーススナップショットを作成し、storage.json と共に ZIP 圧縮して
    AWS S3 へ暗号化アップロードします。
.EXAMPLE
    # タスクスケジューラでの実行例:
    # powershell.exe -ExecutionPolicy Bypass -NoProfile -File "C:\minihub\scripts\backup.ps1" -BucketName "my-backup-bucket"
#>

[CmdletBinding()]
param (
    [string]$AppDir      = "C:\minihub",
    [string]$ConfigFile  = "minihub.json",
    [Parameter(Mandatory = $false)]
    [string]$BucketName  = "beppu-ih-backup",
    [string]$S3Prefix    = "minihub",
    [string]$Region      = "ap-northeast-1",
    # 暗号化方式: Amazon S3 マネージドキー (SSE-S3) の場合は "AES256"、KMS の場合は "aws:kms"
    [string]$SSE         = "AES256"
)

$ErrorActionPreference = "Stop"

# --- パス定義 ---
$BinaryPath = Join-Path $AppDir "minihub.exe"
$ConfigPath = Join-Path $AppDir $ConfigFile
$LogDir     = Join-Path $AppDir "logs"
$TempDir    = Join-Path $AppDir "temp\backup"

# ログディレクトリおよび一時ディレクトリの作成
if (-not (Test-Path $LogDir)) {
    New-Item -ItemType Directory -Force -Path $LogDir | Out-Null
}
if (-not (Test-Path $TempDir)) {
    New-Item -ItemType Directory -Force -Path $TempDir | Out-Null
}

$LogFile = Join-Path $LogDir "backup.log"

# 前回ログクリア
if (Test-Path $LogFile) {
    Clear-Content $LogFile
}

# --- ログ出力ヘルパー ---
function Write-Log {
    param(
        [string]$Message,
        [ValidateSet("INFO", "WARN", "ERROR")][string]$Level = "INFO"
    )
    $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    $line = "[$timestamp] [$Level] $Message"
    
    switch ($Level) {
        "INFO"  { Write-Host $line -ForegroundColor Cyan }
        "WARN"  { Write-Host $line -ForegroundColor Yellow }
        "ERROR" { Write-Host $line -ForegroundColor Red }
    }
    
    try {
        Add-Content -Path $LogFile -Value $line -Encoding UTF8
    } catch {
        # ログ書き込み失敗時も処理は継続
    }
}

# --- メイン処理 ---
try {
    Write-Log "============================================="
    Write-Log "minihub バックアップ処理を開始します"
    Write-Log "アプリケーション: $AppDir"
    Write-Log "設定ファイル: $ConfigPath"
    Write-Log "転送先: s3://$BucketName/$S3Prefix/"
    Write-Log "暗号化: $SSE"

    # 0. 前提チェック
    if (-not (Test-Path $BinaryPath)) {
        throw "minihub バイナリが見つかりません: $BinaryPath"
    }
    if (-not (Test-Path $ConfigPath)) {
        throw "設定ファイルが見つかりません: $ConfigPath"
    }

    # 設定ファイルから dataDir を取得（未指定なら AppDir\data）
    $dataDir = Join-Path $AppDir "data"
    try {
        $json = Get-Content -Raw -Path $ConfigPath | ConvertFrom-Json
        if ($json.server -and $json.server.dataDir) {
            $configuredData = $json.server.dataDir
            if ([System.IO.Path]::IsPathRooted($configuredData)) {
                $dataDir = $configuredData
            } else {
                $dataDir = Join-Path $AppDir $configuredData
            }
        }
    } catch {
        Write-Log "設定ファイルの解析に失敗したため既定の dataDir を使用します: $dataDir" "WARN"
    }

    $markerFile = Join-Path $dataDir "storage.json"
    if (-not (Test-Path $markerFile)) {
        throw "データディレクトリまたはマーカーが見つかりません: $markerFile"
    }

    $Timestamp    = Get-Date -Format "yyyyMMdd_HHmmss"
    $WorkDir      = Join-Path $TempDir "work_$Timestamp"
    $BackupDbPath = Join-Path $WorkDir "minuhub.db"
    $ZipName      = "minihub_$Timestamp.zip"
    $ZipPath      = Join-Path $TempDir $ZipName
    $S3Dest       = "s3://$BucketName/$S3Prefix/$Timestamp/$ZipName"

    New-Item -ItemType Directory -Force -Path $WorkDir | Out-Null

    # 1. minihub CLI によるホットバックアップ (VACUUM INTO)
    Write-Log "Step 1: 整合性ホットバックアップを作成中: $BackupDbPath"
    $backupOutput = & $BinaryPath backup -config $ConfigPath -out $BackupDbPath 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "minihub backup コマンドが失敗しました (ExitCode: $LASTEXITCODE)`n出力: $backupOutput"
    }
    Write-Log "ホットバックアップ完了: $backupOutput"

    # 2. リストア互換性のため storage.json を同封
    Copy-Item -Path $markerFile -Destination (Join-Path $WorkDir "storage.json") -Force

    # 3. ZIP 圧縮 (minuhub.db + storage.json)
    Write-Log "Step 2: バックアップアーカイブを ZIP 圧縮中: $ZipPath"
    Compress-Archive -Path "$WorkDir\*" -DestinationPath $ZipPath -Force
    Remove-Item -Path $WorkDir -Recurse -Force -ErrorAction SilentlyContinue

    $zipItem = Get-Item $ZipPath
    $zipSizeKB = [math]::Round($zipItem.Length / 1KB, 2)
    Write-Log "圧縮完了: $ZipName ($zipSizeKB KB)"

    # 4. AWS S3 へ暗号化アップロード
    Write-Log "Step 3: S3 へ暗号化アップロード中: $S3Dest"
    $awsArgs = @(
        "s3", "cp",
        $ZipPath,
        $S3Dest,
        "--region", $Region,
        "--no-progress"
    )

    if ($SSE -eq "AES256") {
        $awsArgs += @("--sse", "AES256")
    } elseif ($SSE -eq "aws:kms") {
        $awsArgs += @("--sse", "aws:kms")
    }

    $awsOutput = & aws @awsArgs 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "AWS S3 アップロードに失敗しました (ExitCode: $LASTEXITCODE)`nエラー詳細: $awsOutput"
    }

    Write-Log "S3 アップロード成功: $S3Dest"

    # 5. ローカル一時 ZIP ファイルの削除
    Write-Log "Step 4: ローカル一時ファイルをクリーンアップ中..."
    Remove-Item -Path $ZipPath -Force -ErrorAction SilentlyContinue

    # 6. 古い一時フォルダ残骸があれば掃除 (7日以上前)
    Get-ChildItem -Path $TempDir -Filter "minihub_*.zip" -ErrorAction SilentlyContinue |
        Where-Object { $_.LastWriteTime -lt (Get-Date).AddDays(-7) } |
        Remove-Item -Force -ErrorAction SilentlyContinue
    Get-ChildItem -Path $TempDir -Filter "work_*" -Directory -ErrorAction SilentlyContinue |
        Where-Object { $_.LastWriteTime -lt (Get-Date).AddDays(-1) } |
        Remove-Item -Recurse -Force -ErrorAction SilentlyContinue

    Write-Log "バックアップ処理が正常に完了しました。"
    Write-Log "============================================="
    exit 0
}
catch {
    Write-Log "バックアップ処理が異常終了しました: $_" "ERROR"
    Write-Log "============================================="
    exit 1
}

