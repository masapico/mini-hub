(() => {
    "use strict";

    const $ = (id) => document.getElementById(id);

    const State = {
        open: false,
        activeAccountID: "",
        accounts: [],
        messages: [],
        quota: { dailyQuota: 0, dailyUsed: 0, enabled: true },
        stagedAttachments: [],
        resizing: false,
    };

    function storageKey() {
        const userID = window.S?.me?.id;
        return userID ? `minihub_ai_canvas_${userID}` : null;
    }

    function legacyStorageKey() {
        const userID = window.S?.me?.id;
        return userID ? `minihub_ai_board_${userID}` : null;
    }

    function loadSavedMessages() {
        const key = storageKey();
        if (!key) return [];
        try {
            let raw = localStorage.getItem(key);
            // Migrate legacy whiteboard messages if new key not found
            if (!raw) {
                const legacyKey = legacyStorageKey();
                if (legacyKey) {
                    raw = localStorage.getItem(legacyKey);
                    if (raw) {
                        localStorage.setItem(key, raw);
                        localStorage.removeItem(legacyKey);
                    }
                }
            }
            if (!raw) return [];
            const parsed = JSON.parse(raw);
            return Array.isArray(parsed) ? parsed : [];
        } catch {
            return [];
        }
    }

    function saveMessages(msgs) {
        const key = storageKey();
        if (!key) return;
        try {
            // Keep up to 30 latest messages to prevent storage explosion (exclude thinking and errors)
            const trimmed = msgs.filter(m => !m.error && !m.thinking).slice(-30);
            localStorage.setItem(key, JSON.stringify(trimmed));
        } catch {
            // ignore storage quota errors
        }
    }

    function formatQuota(quota) {
        if (!quota || quota.dailyQuota === 0) {
            return quota?.dailyUsed ? `本日利用: ${quota.dailyUsed}回 (無制限)` : "";
        }
        const remaining = Math.max(0, quota.dailyQuota - quota.dailyUsed);
        return `本日残り: ${remaining} / ${quota.dailyQuota}回`;
    }

    async function fetchQuota() {
        try {
            const res = await window.api("/api/ai/quota");
            if (res) {
                State.quota = res;
                updateQuotaDisplay();
            }
        } catch {
            // ignore
        }
    }

    function updateQuotaDisplay() {
        const statusEl = $("aiCanvasQuotaStatus");
        if (statusEl) {
            statusEl.textContent = formatQuota(State.quota);
            const q = State.quota;
            const low = !!q && q.dailyQuota > 0 && (q.dailyQuota - q.dailyUsed) / q.dailyQuota <= 0.2;
            statusEl.classList.toggle("quota-low", low);
        }
    }

    function getAssistantDisplayName(msg) {
        if (!msg) return "AI";
        const accounts = State.accounts.length ? State.accounts : (window.S?.aiAccounts || []);
        if (msg.accountId) {
            const found = accounts.find(a => a.id === msg.accountId);
            if (found?.name) return found.name;
        }
        if (msg.accountName) {
            const foundById = accounts.find(a => a.id === msg.accountName);
            if (foundById?.name) return foundById.name;
            const foundByName = accounts.find(a => a.name === msg.accountName);
            if (foundByName?.name) return foundByName.name;
            return msg.accountName;
        }
        const active = accounts.find(a => a.id === State.activeAccountID);
        return active?.name || "AI";
    }

    function formatMsgTime(isoStr) {
        if (!isoStr) return "";
        try {
            const d = new Date(isoStr);
            if (isNaN(d.getTime())) return "";
            return d.toLocaleTimeString("ja-JP", { hour: "2-digit", minute: "2-digit" });
        } catch {
            return "";
        }
    }

    function convertMarkdownToWordHtml(markdownText) {
        if (!markdownText) return "";
        let html = "";
        if (typeof marked !== "undefined" && marked.parse) {
            try {
                html = marked.parse(markdownText, { gfm: true, breaks: true });
            } catch {
                html = "";
            }
        }
        if (!html) {
            const escapeHtml = (str) =>
                str.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
            html = markdownText.split("\n").map(l => `<p>${escapeHtml(l)}</p>`).join("");
        }

        const tempDiv = document.createElement("div");
        tempDiv.innerHTML = html;

        // Apply Word-compatible inline styling
        tempDiv.querySelectorAll("table").forEach(table => {
            table.setAttribute("border", "1px");
            table.style.borderCollapse = "collapse";
            table.style.width = "100%";
            table.style.margin = "8pt 0";
        });
        tempDiv.querySelectorAll("th").forEach(th => {
            th.style.border = "1px solid #94a3b8";
            th.style.padding = "6pt 8pt";
            th.style.backgroundColor = "#f1f5f9";
            th.style.fontWeight = "bold";
            th.style.textAlign = "left";
        });
        tempDiv.querySelectorAll("td").forEach(td => {
            td.style.border = "1px solid #cbd5e1";
            td.style.padding = "6pt 8pt";
        });
        tempDiv.querySelectorAll("blockquote").forEach(bq => {
            bq.style.borderLeft = "3px solid #cbd5e1";
            bq.style.paddingLeft = "10pt";
            bq.style.marginLeft = "0";
            bq.style.color = "#475569";
        });
        tempDiv.querySelectorAll("pre").forEach(pre => {
            pre.style.backgroundColor = "#f8fafc";
            pre.style.border = "1px solid #e2e8f0";
            pre.style.padding = "8pt";
            pre.style.fontFamily = "Consolas, 'Courier New', monospace";
            pre.style.fontSize = "10pt";
        });
        tempDiv.querySelectorAll("code").forEach(code => {
            if (code.parentElement?.tagName !== "PRE") {
                code.style.backgroundColor = "#f1f5f9";
                code.style.padding = "1pt 4pt";
                code.style.fontFamily = "Consolas, 'Courier New', monospace";
                code.style.fontSize = "10pt";
            }
        });

        return tempDiv.innerHTML;
    }

    async function copyMessageToClipboard(text, buttonEl) {
        const htmlContent = convertMarkdownToWordHtml(text);
        let copied = false;

        if (navigator.clipboard?.write && window.ClipboardItem) {
            try {
                const htmlBlob = new Blob([htmlContent], { type: "text/html" });
                const textBlob = new Blob([text], { type: "text/plain" });
                await navigator.clipboard.write([
                    new ClipboardItem({
                        "text/html": htmlBlob,
                        "text/plain": textBlob,
                    }),
                ]);
                copied = true;
            } catch (e) {
                console.warn("ClipboardItem write failed:", e);
            }
        }

        if (!copied) {
            if (navigator.clipboard?.writeText) {
                try {
                    await navigator.clipboard.writeText(text);
                    copied = true;
                } catch (_) {}
            }
            if (!copied && window.fallbackCopyText) {
                window.fallbackCopyText(text);
                copied = true;
            }
        }

        if (copied) {
            window.note?.("クリップボードにコピーしました（Word貼り付け対応）");
            if (buttonEl) {
                const origContent = Array.from(buttonEl.childNodes);
                buttonEl.innerHTML = "";
                buttonEl.append(window.svgIcon("check"), document.createTextNode(" コピー完了"));
                buttonEl.classList.add("copied");
                setTimeout(() => {
                    buttonEl.innerHTML = "";
                    origContent.forEach(node => buttonEl.append(node));
                    buttonEl.classList.remove("copied");
                }, 2000);
            }
        } else {
            window.note?.("コピーに失敗しました", true);
        }
    }

    function renderMessage(msg) {
        const isAssistant = msg.role === "assistant";
        const isThinking = !!msg.thinking;
        const wrap = document.createElement("div");
        wrap.className = `ai-canvas-msg ${msg.role}${isThinking ? " thinking" : ""}`;

        const meta = document.createElement("div");
        meta.className = "ai-canvas-msg-meta";

        const senderSpan = document.createElement("span");
        senderSpan.className = "ai-canvas-msg-sender";
        const roleIcon = isAssistant ? window.svgIcon("robot") : window.svgIcon("person");
        roleIcon.classList.add("ai-canvas-role-icon");
        senderSpan.append(roleIcon);
        const nameText = isAssistant ? getAssistantDisplayName(msg) : "あなた";
        senderSpan.append(document.createTextNode(` ${nameText}`));
        meta.append(senderSpan);

        const timeStr = formatMsgTime(msg.ts);
        if (timeStr) {
            const timeEl = document.createElement("time");
            timeEl.className = "ai-canvas-msg-time";
            timeEl.textContent = timeStr;
            meta.append(timeEl);
        }

        wrap.append(meta);

        const body = document.createElement("div");
        body.className = "ai-canvas-msg-body";
        if (isThinking) {
            const indicator = document.createElement("div");
            indicator.className = "ai-thinking-indicator";
            const spinner = document.createElement("span");
            spinner.className = "spinner-border spinner-border-sm text-primary";
            spinner.setAttribute("role", "status");
            spinner.setAttribute("aria-hidden", "true");
            const textSpan = document.createElement("span");
            textSpan.textContent = "AIが回答を生成しています…";
            indicator.append(spinner, textSpan);
            body.append(indicator);
            wrap.append(body);
            return wrap;
        }
        if (isAssistant) {
            if (typeof window.renderMessageMarkdown === "function") {
                body.append(window.renderMessageMarkdown(msg.content));
            } else if (window.marked?.parse) {
                try {
                    const mdContainer = document.createElement("div");
                    mdContainer.className = "markdown-body";
                    mdContainer.innerHTML = window.marked.parse(msg.content);
                    body.append(mdContainer);
                } catch {
                    body.textContent = msg.content;
                }
            } else {
                body.textContent = msg.content;
            }
        } else {
            body.textContent = msg.content;
        }
        wrap.append(body);

        if (isAssistant && msg.error) {
            wrap.classList.add("error");
            if (!msg.retryQuota && msg.retryText) {
                const actions = document.createElement("div");
                actions.className = "ai-canvas-msg-actions";
                const retryBtn = document.createElement("button");
                retryBtn.type = "button";
                retryBtn.className = "ai-canvas-action-btn";
                retryBtn.title = "同じ内容でもう一度送信";
                retryBtn.append(window.svgIcon("arrow-clockwise"), document.createTextNode(" 再試行"));
                retryBtn.onclick = () => {
                    const idx = State.messages.indexOf(msg);
                    if (idx < 0) return;
                    const removeFrom = idx > 0 && State.messages[idx - 1].role === "user" ? idx - 1 : idx;
                    State.messages.splice(removeFrom, idx - removeFrom + 1);
                    const input = $("aiCanvasInput");
                    if (!input) return;
                    input.innerText = msg.retryText;
                    handleSend();
                };
                actions.append(retryBtn);
                wrap.append(actions);
            }
        } else if (isAssistant) {
            const actions = document.createElement("div");
            actions.className = "ai-canvas-msg-actions";

            // 1. Copy (Word compatible)
            const copyBtn = document.createElement("button");
            copyBtn.type = "button";
            copyBtn.className = "ai-canvas-action-btn";
            copyBtn.title = "クリップボードにコピー（Word貼り付け対応）";
            copyBtn.append(window.svgIcon("clipboard"), document.createTextNode(" コピー"));
            copyBtn.onclick = () => copyMessageToClipboard(msg.content, copyBtn);
            actions.append(copyBtn);

            // 2. Insert into composer
            const insertBtn = document.createElement("button");
            insertBtn.type = "button";
            insertBtn.className = "ai-canvas-action-btn";
            insertBtn.title = "メッセージ入力欄に挿入";
            insertBtn.append(window.svgIcon("box-arrow-in-down-left"), document.createTextNode(" 入力欄に挿入"));
            insertBtn.onclick = () => {
                insertIntoComposer(msg.content);
            };
            actions.append(insertBtn);

            wrap.append(actions);
        }

        return wrap;
    }

    function renderAllMessages() {
        const container = $("aiCanvasMessages");
        if (!container) return;
        container.innerHTML = "";

        if (State.messages.length === 0) {
            const welcome = document.createElement("div");
            welcome.className = "ai-canvas-welcome p-3 text-muted small";
            welcome.innerHTML = `<p class="mb-1">自分専用の作業・下書き用キャンバスです。ここでの対話は他ユーザーには公開されません。</p><p class="mb-0">下書きの推敲や質問を行い、「コピー」や「入力欄に挿入」でチームチャットに活用できます。</p>`;
            container.append(welcome);
            return;
        }

        State.messages.forEach((m) => {
            container.append(renderMessage(m));
        });
        container.scrollTop = container.scrollHeight;
    }

    function insertIntoComposer(text) {
        // Determine whether to insert into thread input or main input
        const threadPanel = $("threadPanel");
        const isThreadOpen = threadPanel && !threadPanel.classList.contains("hidden");
        const targetInput = isThreadOpen ? $("threadInput") : $("input");

        if (!targetInput) return;
        targetInput.focus();
        if (targetInput.textContent.trim()) {
            targetInput.textContent = targetInput.textContent + "\n\n" + text;
        } else {
            targetInput.textContent = text;
        }
        targetInput.dispatchEvent(new Event("input", { bubbles: true }));
        window.note?.("入力欄に下書きを挿入しました");
    }


    async function updateAccountsDropdown() {
        const select = $("aiCanvasAccountSelect");
        const toggleBtn = $("aiCanvasToggle");
        const sidebarBtn = $("sidebarCanvasBtn");
        if (!select) return;

        const isFeatureEnabled = document.body.dataset.aiCanvasEnabled !== "false";
        if (!isFeatureEnabled) {
            if (toggleBtn) toggleBtn.classList.add("hidden");
            if (sidebarBtn) sidebarBtn.classList.add("hidden");
            if (State.open) toggleCanvas(false);
            return;
        }

        // Fetch accounts autonomously if not yet available in window.S
        let accounts = window.S?.aiAccounts;
        if (!accounts || accounts.length === 0) {
            try {
                accounts = await window.api("/api/ai-accounts");
                if (window.S) window.S.aiAccounts = accounts;
            } catch {
                accounts = [];
            }
        }
        State.accounts = Array.isArray(accounts) ? accounts : [];

        if (!accounts || accounts.length === 0) {
            if (toggleBtn) toggleBtn.classList.add("hidden");
            if (sidebarBtn) sidebarBtn.classList.add("hidden");
            if (State.open) toggleCanvas(false);
            return;
        }

        if (toggleBtn) toggleBtn.classList.remove("hidden");
        if (sidebarBtn) sidebarBtn.classList.remove("hidden");

        const prev = select.value || State.activeAccountID;
        select.innerHTML = "";
        const sortedAccounts = [...accounts].sort((a, b) =>
            (a.name || "").localeCompare(b.name || "", "ja") || a.id.localeCompare(b.id));
        sortedAccounts.forEach((a) => {
            const opt = document.createElement("option");
            opt.value = a.id;
            opt.textContent = `${a.name} (${a.id})`;
            select.append(opt);
        });

        if (sortedAccounts.some(a => a.id === prev)) {
            select.value = prev;
            State.activeAccountID = prev;
        } else if (sortedAccounts.length > 0) {
            State.activeAccountID = sortedAccounts[0].id;
            select.value = State.activeAccountID;
        }
    }

    function toggleCanvas(forceOpen) {
        const panel = $("aiCanvasPanel");
        const toggleBtn = $("aiCanvasToggle");
        const sidebarBtn = $("sidebarCanvasBtn");
        const app = document.querySelector(".app");
        if (!panel) return;

        const shouldOpen = forceOpen !== undefined ? forceOpen : !State.open;
        State.open = shouldOpen;

        panel.classList.toggle("hidden", !shouldOpen);
        if (toggleBtn) {
            toggleBtn.setAttribute("aria-expanded", String(shouldOpen));
            toggleBtn.classList.toggle("active", shouldOpen);
        }
        if (sidebarBtn) {
            sidebarBtn.classList.toggle("active", shouldOpen);
        }
        if (app) {
            app.classList.toggle("canvas-open", shouldOpen);
        }

        if (shouldOpen) {
            // Close thread panel if open
            if (window.closeThreadPanel) {
                window.closeThreadPanel();
            } else {
                $("threadPanel")?.classList.add("hidden");
                app?.classList.remove("thread-open");
            }
            State.messages = loadSavedMessages();
            renderAllMessages();
            fetchQuota();
            updateAccountsDropdown();
            $("aiCanvasInput")?.focus();
        }
    }

    async function handleSend(e) {
        e?.preventDefault();
        const input = $("aiCanvasInput");
        const sendBtn = $("aiCanvasSend");
        const statusEl = $("aiCanvasStatus");
        if (!input || !sendBtn) return;

        const text = input.innerText.trim();
        const attachments = State.stagedAttachments || [];
        if (!text && attachments.length === 0) return;

        const accountID = $("aiCanvasAccountSelect")?.value || State.activeAccountID;
        if (!accountID) {
            window.note?.("AIモデルを選択してください", true);
            return;
        }

        const accounts = State.accounts.length ? State.accounts : (window.S?.aiAccounts || []);
        const selectedAccount = accounts.find(a => a.id === accountID);
        const accountName = selectedAccount?.name || accountID;

        // User message
        const userMsg = { role: "user", content: text, ts: new Date().toISOString() };
        State.messages.push(userMsg);

        // Thinking placeholder for assistant
        const thinkingMsg = {
            role: "assistant",
            thinking: true,
            accountId: accountID,
            accountName: accountName,
            ts: new Date().toISOString(),
        };
        State.messages.push(thinkingMsg);
        renderAllMessages();

        // Clear input and files
        input.innerText = "";
        clearStagedFiles();

        input.contentEditable = "false";
        sendBtn.disabled = true;
        sendBtn.innerHTML = '<span class="spinner-border spinner-border-sm me-1" role="status" aria-hidden="true"></span>送信中…';
        if (statusEl) statusEl.textContent = "AIが回答を生成しています…";

        try {
            // Prepare messages payload for sliding window (excluding errors and temporary thinking)
            const payloadMessages = State.messages.filter(m => !m.error && !m.thinking).map(m => ({
                role: m.role,
                content: m.content,
            }));

            const res = await window.api("/api/ai/direct", {
                method: "POST",
                body: JSON.stringify({
                    aiAccountId: accountID,
                    messages: payloadMessages,
                    attachments: attachments.map(a => ({ name: a.name, data: a.base64 })),
                }),
            });

            const reply = res.reply || "";
            const replyMsg = {
                role: "assistant",
                content: reply,
                accountId: accountID,
                accountName: accountName,
                ts: new Date().toISOString(),
            };
            const thinkingIdx = State.messages.indexOf(thinkingMsg);
            if (thinkingIdx >= 0) {
                State.messages[thinkingIdx] = replyMsg;
            } else {
                State.messages.push(replyMsg);
            }
            saveMessages(State.messages);
            renderAllMessages();

            if (res.dailyQuota !== undefined) {
                State.quota = {
                    dailyQuota: res.dailyQuota,
                    dailyUsed: res.dailyUsed,
                    enabled: true,
                };
                updateQuotaDisplay();
            }
        } catch (err) {
            const isQuota = err.status === 429 || (err.message && err.message.includes("上限"));
            const errMsg = isQuota ? "本日のAI利用上限に達しました。明日再度お試しください。" : `AI応答エラー: ${err.message}`;
            const errorMsg = {
                role: "assistant",
                content: `⚠️ ${errMsg}`,
                accountId: accountID,
                accountName: "システム",
                ts: new Date().toISOString(),
                error: true,
                retryText: text,
                retryQuota: isQuota,
            };
            const thinkingIdx = State.messages.indexOf(thinkingMsg);
            if (thinkingIdx >= 0) {
                State.messages[thinkingIdx] = errorMsg;
            } else {
                State.messages.push(errorMsg);
            }
            renderAllMessages();
        } finally {
            input.contentEditable = "true";
            sendBtn.disabled = false;
            sendBtn.textContent = "送信";
            if (statusEl) statusEl.textContent = "";
            input.focus();
        }
    }

    function clearStagedFiles() {
        State.stagedAttachments = [];
        const container = $("aiCanvasAttachedFiles");
        if (container) {
            container.innerHTML = "";
            container.classList.add("hidden");
        }
        const fileInput = $("aiCanvasAttachFileInput");
        if (fileInput) fileInput.value = "";
    }

    function renderStagedFiles() {
        const container = $("aiCanvasAttachedFiles");
        if (!container) return;
        container.innerHTML = "";
        if (State.stagedAttachments.length === 0) {
            container.classList.add("hidden");
            return;
        }
        container.classList.remove("hidden");
        State.stagedAttachments.forEach((f, idx) => {
            const badge = document.createElement("span");
            badge.className = "badge bg-secondary me-1 mb-1 d-inline-flex align-items-center gap-1";
            badge.append(window.svgIcon("file-earmark"), document.createTextNode(f.name));
            const removeBtn = document.createElement("button");
            removeBtn.type = "button";
            removeBtn.className = "btn-close btn-close-white";
            removeBtn.style.fontSize = "0.6rem";
            removeBtn.onclick = () => {
                State.stagedAttachments.splice(idx, 1);
                renderStagedFiles();
            };
            badge.append(removeBtn);
            container.append(badge);
        });
    }

    function handleFileSelection(files) {
        const maxFiles = 5;
        const maxSingle = 20 * 1024 * 1024;
        const maxTotal = 50 * 1024 * 1024;

        if (State.stagedAttachments.length + files.length > maxFiles) {
            window.note?.("添付できるファイルは最大5件です", true);
            return;
        }

        let currentTotal = State.stagedAttachments.reduce((acc, f) => acc + f.size, 0);

        Array.from(files).forEach((file) => {
            if (file.size > maxSingle) {
                window.note?.(`ファイルサイズは20MB以内です (${file.name})`, true);
                return;
            }
            if (currentTotal + file.size > maxTotal) {
                window.note?.("ファイル合計サイズが50MBを超えています", true);
                return;
            }
            currentTotal += file.size;

            const reader = new FileReader();
            reader.onload = () => {
                const b64 = reader.result.split(",")[1];
                State.stagedAttachments.push({
                    name: file.name,
                    size: file.size,
                    base64: b64,
                });
                renderStagedFiles();
            };
            reader.readAsDataURL(file);
        });
    }

    function setupResizer() {
        const resizer = $("aiCanvasResizer");
        if (!resizer) return;

        let startX = 0;
        let startWidth = 0;

        const onMouseMove = (e) => {
            if (!State.resizing) return;
            const delta = startX - e.clientX;
            const newWidth = Math.max(320, Math.min(800, startWidth + delta));
            document.documentElement.style.setProperty("--canvas-width", `${newWidth}px`);
        };

        const onMouseUp = () => {
            State.resizing = false;
            document.body.classList.remove("thread-col-resizing");
            resizer.classList.remove("is-resizing");
            window.removeEventListener("mousemove", onMouseMove);
            window.removeEventListener("mouseup", onMouseUp);
        };

        resizer.addEventListener("mousedown", (e) => {
            State.resizing = true;
            startX = e.clientX;
            const panel = $("aiCanvasPanel");
            startWidth = panel ? panel.getBoundingClientRect().width : 440;
            document.body.classList.add("thread-col-resizing");
            resizer.classList.add("is-resizing");
            window.addEventListener("mousemove", onMouseMove);
            window.addEventListener("mouseup", onMouseUp);
        });

        resizer.addEventListener("dblclick", () => {
            document.documentElement.style.setProperty("--canvas-width", "var(--canvas-default-width)");
        });
    }

    function init() {
        const toggleBtn = $("aiCanvasToggle");
        const sidebarBtn = $("sidebarCanvasBtn");
        const closeBtn = $("aiCanvasClose");
        const resetBtn = $("aiCanvasResetBtn");
        const form = $("aiCanvasForm");
        const input = $("aiCanvasInput");
        const attachBtn = $("aiCanvasAttachBtn");
        const fileInput = $("aiCanvasAttachFileInput");

        if (toggleBtn) {
            toggleBtn.onclick = () => toggleCanvas();
        }
        if (sidebarBtn) {
            sidebarBtn.onclick = () => toggleCanvas();
        }
        if (closeBtn) {
            closeBtn.onclick = () => toggleCanvas(false);
        }
        if (resetBtn) {
            resetBtn.onclick = () => {
                if (confirm("会話履歴をリセットして、新しい会話を開始しますか？")) {
                    State.messages = [];
                    saveMessages([]);
                    renderAllMessages();
                    window.note?.("会話履歴をリセットしました", false, false);
                }
            };
        }

        if (form) {
            form.onsubmit = handleSend;
        }

        if (input) {
            input.onkeydown = (e) => {
                if ((e.ctrlKey || e.metaKey) && e.key === "Enter") {
                    e.preventDefault();
                    handleSend();
                }
            };
        }

        if (attachBtn && fileInput) {
            attachBtn.onclick = () => fileInput.click();
            fileInput.onchange = (e) => {
                if (e.target.files?.length) {
                    handleFileSelection(e.target.files);
                }
            };
        }

        // Shortcut: Ctrl/Cmd + Shift + A to toggle
        window.addEventListener("keydown", (e) => {
            if ((e.ctrlKey || e.metaKey) && e.shiftKey && (e.key === "A" || e.key === "a")) {
                e.preventDefault();
                toggleCanvas();
            }
        });

        setupResizer();
        updateAccountsDropdown();
        window.addEventListener("focus", () => updateAccountsDropdown());
    }

    // Expose helpers for app.js integration
    window.updateAICanvasAccounts = updateAccountsDropdown;
    window.resetAICanvas = () => {
        State.messages = [];
        toggleCanvas(false);
        renderAllMessages();
    };

    // Backward compatibility aliases
    window.updateAIWhiteboardAccounts = window.updateAICanvasAccounts;
    window.resetAIWhiteboard = window.resetAICanvas;

    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", init);
    } else {
        init();
    }
})();

