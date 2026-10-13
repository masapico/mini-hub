(() => {
    "use strict";

    const $ = (id) => document.getElementById(id);

    const State = {
        open: false,
        activeAccountID: "",
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
            // Keep up to 30 latest messages to prevent storage explosion
            const trimmed = msgs.slice(-30);
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
        }
    }

    function renderMessage(msg) {
        const wrap = document.createElement("div");
        wrap.className = `ai-canvas-msg ${msg.role}`;

        const meta = document.createElement("div");
        meta.className = "ai-canvas-msg-meta";
        const roleIcon = msg.role === "assistant" ? window.svgIcon("robot") : window.svgIcon("person");
        meta.append(roleIcon);
        meta.append(document.createTextNode(msg.role === "assistant" ? (msg.accountName || "AI") : "あなた"));
        wrap.append(meta);

        const body = document.createElement("div");
        body.className = "ai-canvas-msg-body";
        if (msg.role === "assistant") {
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

        if (msg.role === "assistant") {
            const actions = document.createElement("div");
            actions.className = "ai-canvas-msg-actions";

            // 1. Copy
            const copyBtn = document.createElement("button");
            copyBtn.type = "button";
            copyBtn.className = "ai-canvas-action-btn";
            copyBtn.title = "クリップボードにコピー";
            copyBtn.append(window.svgIcon("clipboard"), document.createTextNode(" コピー"));
            copyBtn.onclick = () => {
                if (navigator.clipboard?.writeText) {
                    navigator.clipboard.writeText(msg.content).then(() => {
                        window.note?.("クリップボードにコピーしました");
                    });
                } else {
                    window.fallbackCopyText?.(msg.content);
                    window.note?.("クリップボードにコピーしました");
                }
            };
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

            // 3. Share to channel
            const shareBtn = document.createElement("button");
            shareBtn.type = "button";
            shareBtn.className = "ai-canvas-action-btn";
            shareBtn.title = "現在のチャンネルに共有";
            shareBtn.append(window.svgIcon("chat-quote"), document.createTextNode(" チャンネルに共有"));
            shareBtn.onclick = () => {
                shareToCurrentChannel(msg.content, msg.accountName);
            };
            actions.append(shareBtn);

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
            welcome.innerHTML = `<p class="mb-1">自分専用の作業・下書き用キャンバスです。ここでの対話は他ユーザーには公開されません。</p><p class="mb-0">下書きの推敲や質問を行い、「入力欄に挿入」や「チャンネルに共有」でチームに還元できます。</p>`;
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

    async function shareToCurrentChannel(text, accountName) {
        const currentChannel = window.S?.channel;
        if (!currentChannel) {
            window.note?.("共有先のチャンネルを選択してください", true);
            return;
        }

        const quote = `> 💡 **AIキャンバスのメモを共有しました** (${accountName || "AI"})\n> \n` +
            text.split("\n").map(l => `> ${l}`).join("\n");

        if (!confirm(`現在のチャンネル「${currentChannel.name}」にこの回答を共有しますか？`)) {
            return;
        }

        try {
            await window.api(`/api/channels/${encodeURIComponent(currentChannel.id)}/messages`, {
                method: "POST",
                body: JSON.stringify({ text: quote }),
            });
            window.note?.(`チャンネル「${currentChannel.name}」に共有しました`);
        } catch (err) {
            window.note?.(`共有に失敗しました: ${err.message}`, true);
        }
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
        accounts.forEach((a) => {
            const opt = document.createElement("option");
            opt.value = a.id;
            opt.textContent = `${a.name} (${a.id})`;
            select.append(opt);
        });

        if (accounts.some(a => a.id === prev)) {
            select.value = prev;
            State.activeAccountID = prev;
        } else {
            State.activeAccountID = accounts[0].id;
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

        const selectedAccount = window.S?.aiAccounts?.find(a => a.id === accountID);
        const accountName = selectedAccount?.name || accountID;

        // User message
        const userMsg = { role: "user", content: text, ts: new Date().toISOString() };
        State.messages.push(userMsg);
        renderAllMessages();

        // Clear input and files
        input.innerText = "";
        clearStagedFiles();

        input.contentEditable = "false";
        sendBtn.disabled = true;
        if (statusEl) statusEl.textContent = "AI考え中...";

        try {
            // Prepare messages payload for sliding window
            const payloadMessages = State.messages.map(m => ({
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
            State.messages.push({
                role: "assistant",
                content: reply,
                accountName: accountName,
                ts: new Date().toISOString(),
            });
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
            State.messages.push({
                role: "assistant",
                content: `⚠️ ${errMsg}`,
                accountName: "システム",
                ts: new Date().toISOString(),
            });
            renderAllMessages();
        } finally {
            input.contentEditable = "true";
            sendBtn.disabled = false;
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
                    window.note?.("会話履歴をリセットしました");
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

