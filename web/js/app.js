const S = {
        csrf: "",
        me: null,
        channels: [],
        channel: null,
        msgs: [],
        ws: null,
        unread: new Map(),
        groups: new Map(),
		aiAccounts: [],
		managedAIAccounts: [],
		selectedAIID: "",
		stagedAttachments: [],
    },
    $ = (x) => document.getElementById(x),
    nameSegmenter = typeof Intl.Segmenter === "function"
        ? new Intl.Segmenter("ja", { granularity: "grapheme" }) : null,
    ini = (x) => {
        const name = (x || "").trim() || "?";
        const first = (value) => nameSegmenter
            ? nameSegmenter.segment(value)[Symbol.iterator]().next().value.segment
            : Array.from(value)[0];
        return first(first(name).toUpperCase());
    },
    icon = (name) => name === "lock-fill"
        ? '<svg class="bi" viewBox="0 0 16 16" aria-hidden="true"><path fill-rule="evenodd" d="M8 0a4 4 0 0 1 4 4v2.05a2.5 2.5 0 0 1 2 2.45v5a2.5 2.5 0 0 1-2.5 2.5h-7A2.5 2.5 0 0 1 2 13.5v-5a2.5 2.5 0 0 1 2-2.45V4a4 4 0 0 1 4-4m0 1a3 3 0 0 0-3 3v2h6V4a3 3 0 0 0-3-3"/></svg>'
        : `<svg class="bi" aria-hidden="true"><use href="${Minihub.url("/vendor/bootstrap-icons/bootstrap-icons.svg")}#${name}"></use></svg>`,
    directMember = (c) => c?.members?.includes(S.me?.id),
    groupMember = (c) =>
        c?.groups?.some((id) => S.me?.groups?.includes(id)),
    member = (c) => directMember(c) || groupMember(c);
window.S = S;

function messageDisplayText(message) {
	if (message.withdrawnAt) return "取り消されました";
    const legacyPollPrefix = "投票を開始しました: ";
    return message.pollRef && message.text.startsWith(legacyPollPrefix)
        ? "アンケートを開始しました: " + message.text.slice(legacyPollPrefix.length)
        : message.text;
}

function messageAuthor(message) {
    return message.ai ? `🤖 ${message.ai.name}` : S.directory?.get(message.userId) || message.userId;
}

function aiCandidates(query = "") {
    const normalized = query.trim().toLocaleLowerCase("ja-JP");
    return S.aiAccounts.filter(a => !normalized || `${a.id} ${a.name}`.toLocaleLowerCase("ja-JP").includes(normalized))
        .map(a => ({id: `ai:${a.id}`, name: a.name}))
        .sort((a, b) => a.id.localeCompare(b.id));
}

function withdrawnNotice(message) {
	const box=document.createElement("span");box.className="withdrawn-notice";
	const mark=document.createElement("span");mark.className="withdrawn-notice-icon";mark.innerHTML=icon("slash-circle");
	const label=document.createElement("span");
	label.textContent=message.withdrawnKind==="poll"?"アンケートは取り消されました":message.withdrawnKind==="schedule"?"予定調整は取り消されました":"このメッセージは取り消されました";
	box.append(mark,label);return box;
}

function renderChannelAction(channel = S.channel) {
    const button = $("join"),
        menu = $("channelMenu"),
        inherited = !!groupMember(channel),
        direct = !!directMember(channel),
        available = !!channel && channel.type === "public" && !inherited;
    button.classList.toggle("hidden", !available || direct);
    menu?.classList.toggle("hidden", !available || !direct);
    button.classList.add("join-action");
    const action = "チャンネルに参加";
    button.textContent = action;
    button.setAttribute("aria-label", action);
    button.title = action;
    button.disabled = !channel || S.channelActionBusy;
    const menuButton = $("channelMenuButton"), leave = $("leaveChannel");
    if (menuButton) menuButton.disabled = !channel || S.channelActionBusy;
    if (leave) leave.disabled = !channel || S.channelActionBusy;
}
async function api(path, o = {}) {
    let method = o.method || "GET",
        body = o.body && typeof o.body !== "string" && !(o.body instanceof FormData) ? JSON.stringify(o.body) : o.body,
        headers = {
            ...(o.body
                ? { "Content-Type": "application/json" }
                : {}),
            ...(S.csrf && !["GET", "HEAD"].includes(method)
                ? { "X-CSRF-Token": S.csrf }
                : {}),
        },
        r;
    try {
        r = await fetch(Minihub.url(path), { ...o, headers, body });
    } catch (_) {
        throw Error("通信に失敗しました。ネットワーク接続を確認して、もう一度お試しください");
    }
    if (r.status === 204) return;
    let b = await r.json().catch(() => ({ error: "サーバーからの応答を読み取れませんでした" }));
    if (r.status === 401) {
        location.replace(Minihub.url("/login"));
        throw Error("セッションの有効期限が切れました");
    }
    if (!r.ok) throw Error(b.error || "操作に失敗しました。もう一度お試しください");
    if (typeof absorbThreadSummaries === "function" && method === "GET") absorbThreadSummaries(path, b);
    return b;
}
const allowedAttachmentExts = new Set([
    ".xlsx", ".xls", ".docx", ".doc", ".pptx", ".ppt", ".pdf",
    ".txt", ".csv", ".tsv", ".json", ".md",
]);
function isAllowedAttachmentFile(name) {
    const dot = name.lastIndexOf(".");
    if (dot === -1) return false;
    return allowedAttachmentExts.has(name.slice(dot).toLowerCase());
}
const aiMentionRegex = /(?:^|\s)@ai:[A-Za-z0-9_-]+/;
function textHasAIMention(text) {
    return aiMentionRegex.test(text || "");
}
function countAIMentions(text) {
    if (!text) return 0;
    const matches = text.match(/(?:^|\s)@ai:[A-Za-z0-9_-]+/g);
    return matches ? matches.length : 0;
}
function formatFileSize(bytes) {
    if (bytes == null || isNaN(bytes)) return "";
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
function renderAttachments(attachments) {
    if (!attachments || !attachments.length) return null;
    const container = document.createElement("div");
    container.className = "message-attachments";
    for (const att of attachments) {
        const item = document.createElement("div");
        item.className = "message-attachment-item";
        item.innerHTML = `<span class="message-attachment-icon">${icon("paperclip")}</span><span class="message-attachment-name"></span><span class="message-attachment-size"></span>`;
        const fileName = att.filename || att.name || "添付ファイル";
        item.querySelector(".message-attachment-name").textContent = fileName;
        item.querySelector(".message-attachment-size").textContent = `(${formatFileSize(att.size)})`;
        container.append(item);
    }
    return container;
}
async function uploadAttachment(channelID, file) {
    const formData = new FormData();
    formData.append("file", file);
    const headers = {};
    if (S.csrf) headers["X-CSRF-Token"] = S.csrf;
    let r;
    try {
        r = await fetch(Minihub.url(`/api/channels/${encodeURIComponent(channelID)}/attachments`), {
            method: "POST",
            headers,
            body: formData,
        });
    } catch (_) {
        throw Error("通信に失敗しました。ネットワーク接続を確認して、もう一度お試しください");
    }
    if (r.status === 401) {
        location.replace(Minihub.url("/login"));
        throw Error("セッションの有効期限が切れました");
    }
    const b = await r.json().catch(() => ({ error: "サーバーからの応答を読み取れませんでした" }));
    if (!r.ok) throw Error(b.error || "ファイルのアップロードに失敗しました");
    return b;
}
function renderStagedAttachments() {
    const host = $("attachedFiles");
    if (!host) return;
    host.replaceChildren();
    if (!S.stagedAttachments || !S.stagedAttachments.length) {
        host.classList.add("hidden");
        return;
    }
    host.classList.remove("hidden");
    for (let i = 0; i < S.stagedAttachments.length; i++) {
        const att = S.stagedAttachments[i];
        const chip = document.createElement("div");
        chip.className = "attached-file-chip";
        chip.innerHTML = `<span class="chip-icon">${icon("paperclip")}</span><span class="chip-name"></span><span class="chip-size"></span><button type="button" class="chip-remove" aria-label="添付を解除">&times;</button>`;
        const fileName = att.filename || att.name || "添付ファイル";
        chip.querySelector(".chip-name").textContent = fileName;
        chip.querySelector(".chip-size").textContent = `(${formatFileSize(att.size)})`;
        chip.querySelector(".chip-remove").onclick = () => {
            S.stagedAttachments.splice(i, 1);
            renderStagedAttachments();
            syncEditorState();
        };
        host.append(chip);
    }
}
function note(x, e = false, store = true) {
    const type = typeof e === "string" ? e : e ? "danger" : "success";
    if (store) storeLocalNotification(x, type);
    const n = document.createElement("div");
    n.className = `toast tc-toast tc-toast-${type} align-items-center border-0`;
    n.setAttribute("role", type === "danger" ? "alert" : "status");
    n.setAttribute("aria-live", type === "danger" ? "assertive" : "polite");
    n.innerHTML = '<div class="d-flex"><div class="toast-body"></div><button type="button" class="btn-close btn-close-white me-2 m-auto" data-bs-dismiss="toast" aria-label="閉じる"></button></div>';
    n.querySelector(".toast-body").textContent = x;
    $("toastContainer").append(n);
    const toast = new bootstrap.Toast(n, {
        delay: ["warning", "danger"].includes(type) ? 7000 : 3500,
    });
    n.addEventListener("hidden.bs.toast", () => n.remove());
    toast.show();
}
function setConnectionStatus(label, connected = false) {
    const status = $("status");
    status.textContent = label;
    status.classList.toggle("connected", connected);
    if (!connected) {
        $("onlineUsersBadge")?.classList.add("hidden");
    }
}
let setup, loadChannels, select, render, catchup, realtime;
async function loadGroups() {
    const groups = await api("/api/groups");
    S.groups = new Map(groups.map((group) => [group.id, group.name]));
    document.querySelectorAll(".group-select").forEach((select) => {
        const selected = new Set(
            Array.from(select.selectedOptions, (option) => option.value),
        );
        select.replaceChildren(
            ...groups.map((group) => {
                const option = document.createElement("option");
                option.value = group.id;
                option.textContent = `${group.name} (${group.id})`;
                option.selected = selected.has(group.id);
                return option;
            }),
        );
    });
    return groups;
}
const selectedGroups = (id) =>
    Array.from($(id).selectedOptions, (option) => option.value);
function setSelectedGroups(id, groupIDs) {
    const selected = new Set(groupIDs || []);
    for (const option of $(id).options)
        option.selected = selected.has(option.value);
}
const MAX_MESSAGE_BYTES = 16 * 1024,
    editor = $("input"),
    textEncoder = new TextEncoder(),
    networkPathMode = document.body.dataset.networkPathMode || "copy";

function safeLinkURL(value) {
    const windowsPath = value.replaceAll("¥", "\\");
    if (windowsPath.startsWith("\\\\")) {
        const parts = windowsPath.slice(2).split("\\"),
            host = parts.shift();
        if (!/^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(host || "") || !parts.some(Boolean))
            return "";
        return `file://${host}/${parts.map((part) => encodeURIComponent(part)).join("/")}`;
    }
    try {
        const url = new URL(windowsPath);
        if (["http:", "https:"].includes(url.protocol)) return url.href;
        if (url.protocol === "file:" && url.hostname && url.pathname !== "/") return url.href;
        return "";
    } catch (_) {
        return "";
    }
}

function linkDestinationLabel(href) {
    try {
        const url = new URL(href);
        if (url.protocol !== "file:") return url.href;
        const path = decodeURIComponent(url.pathname).replaceAll("/", "¥");
        return `¥¥${url.hostname}${path}`;
    } catch (_) {
        return href;
    }
}

function networkLocationDetails(href) {
    try {
        const url = new URL(href);
        if (url.protocol !== "file:" || !url.hostname) return null;
        const segments = url.pathname.split("/").filter(Boolean).map((part) => decodeURIComponent(part));
        if (!segments.length) return null;
        const name = segments.at(-1),
            dot = name.lastIndexOf("."),
            extension = dot > 0 && dot < name.length - 1 ? name.slice(dot + 1).toLowerCase() : "";
        return {
            path: `\\\\${url.hostname}\\${segments.join("\\")}`,
            name,
            extension,
        };
    } catch (_) {
        return null;
    }
}

function networkLocationIcon(extension) {
    const types = [
        ["file-earmark-word", new Set(["doc", "docx", "docm", "dot", "dotx", "dotm"])],
        ["file-earmark-excel", new Set(["xls", "xlsx", "xlsm", "xlsb", "xlt", "xltx", "xltm", "csv"])],
        ["file-earmark-pdf", new Set(["pdf"])],
        ["file-earmark-ppt", new Set(["ppt", "pptx", "pptm", "pot", "potx", "pps", "ppsx"])],
        ["file-earmark-image", new Set(["png", "jpg", "jpeg", "gif", "bmp", "tif", "tiff", "webp", "svg"])],
        ["file-earmark-zip", new Set(["zip", "7z", "rar", "tar", "gz"])],
        ["file-earmark-text", new Set(["txt", "md", "rtf", "log"])],
    ];
    if (!extension) return { name: "folder", kind: "folder" };
    const matched = types.find(([, extensions]) => extensions.has(extension));
    return { name: matched?.[0] || "file-earmark", kind: matched?.[0].replace("file-earmark-", "") || "file" };
}

function svgIcon(name) {
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg"),
        use = document.createElementNS("http://www.w3.org/2000/svg", "use");
    svg.classList.add("bi");
    svg.setAttribute("aria-hidden", "true");
    use.setAttribute("href", Minihub.url(`/vendor/bootstrap-icons/bootstrap-icons.svg#${name}`));
    svg.append(use);
    return svg;
}

function fallbackCopyText(text) {
    const input = document.createElement("textarea");
    input.value = text;
    input.setAttribute("readonly", "");
    input.className = "copy-fallback";
    document.body.append(input);
    input.select();
    let copied = false;
    try { copied = document.execCommand("copy"); } catch (_) {}
    input.remove();
    return copied;
}

async function copyNetworkPath(path, pathElement) {
    let copied = false;
    try {
        if (navigator.clipboard?.writeText) {
            await navigator.clipboard.writeText(path);
            copied = true;
        }
    } catch (_) {}
    if (!copied) copied = fallbackCopyText(path);
    if (copied) {
        note("共有パスをコピーしました", false, false);
        return;
    }
    const selection = window.getSelection(), range = document.createRange();
    range.selectNodeContents(pathElement);
    selection.removeAllRanges();
    selection.addRange(range);
    note("コピーできませんでした。選択したパスを手動でコピーしてください。", true, false);
}

function networkLocation(label, href) {
    const details = networkLocationDetails(href);
    if (!details) return null;
    const iconType = networkLocationIcon(details.extension),
        card = document.createElement("span"),
        visual = document.createElement("span"),
        content = document.createElement("span"),
        title = document.createElement("span"),
        path = document.createElement("code"),
        actions = document.createElement("span");
    card.className = `network-location network-location-${iconType.kind}`;
    visual.className = "network-location-icon";
    visual.append(svgIcon(iconType.name));
    content.className = "network-location-content";
    title.className = "network-location-title";
    appendInlineMarkup(title, label);
    path.className = "network-location-path";
    path.textContent = details.path;
    actions.className = "network-location-actions";
    content.append(title, path);
    if (networkPathMode === "open-and-copy") {
        const open = document.createElement("a");
        open.className = "network-location-action";
        open.href = href;
        open.target = "_blank";
        open.rel = "noopener noreferrer";
        open.title = "管理されたブラウザ環境でエクスプローラーを開く";
        open.append(svgIcon("box-arrow-up-right"), document.createTextNode("開く"));
        actions.append(open);
    }
    if (networkPathMode !== "disabled") {
        const copy = document.createElement("button");
        copy.type = "button";
        copy.className = "network-location-action";
        copy.setAttribute("aria-label", `${details.name} の共有パスをコピー`);
        copy.append(svgIcon("copy"), document.createTextNode("コピー"));
        copy.onclick = () => copyNetworkPath(details.path, path);
        actions.append(copy);
    }
    card.append(visual, content);
    if (actions.childElementCount) card.append(actions);
    return card;
}

function appendInlineMarkup(root, text, editable = false) {
    const markers = [
        ["**", "strong"],
        ["__", "u"],
        ["~~", "s"],
        ["`", "code"],
    ];
    let plain = "";
    const flush = () => {
        if (plain) root.append(document.createTextNode(plain));
        plain = "";
    };
    for (let i = 0; i < text.length;) {
        if (text[i] === "\\" && i + 1 < text.length) {
            plain += text[i + 1];
            i += 2;
            continue;
        }
        const link = text.slice(i).match(/^\[((?:\\.|[^\]\n])+)\]\(((?:\\.|[^)\n])+)\)/);
        if (link) {
            const label = link[1],
                href = safeLinkURL(link[2].replace(/\\(.)/g, "$1"));
            if (href) {
                flush();
                const location = editable ? null : networkLocation(label, href);
                if (location) {
                    root.append(location);
                    i += link[0].length;
                    continue;
                }
                const anchor = document.createElement("a");
                anchor.href = href;
                anchor.target = "_blank";
                anchor.rel = "noopener noreferrer";
                anchor.title = `リンク先: ${linkDestinationLabel(href)}`;
                appendInlineMarkup(anchor, label, editable);
                root.append(anchor);
                i += link[0].length;
                continue;
            }
        }
        const mention = text.slice(i).match(/^@(?:(group|ai):)?([A-Za-z0-9][A-Za-z0-9_-]{0,63})(?:（([^\n）]*)）)?/);
        if (mention) {
            flush();
            const isGroup = mention[1] === "group",
                isAI = mention[1] === "ai",
                id = mention[2],
                span = document.createElement("span");
            span.className = "mention" + (isAI ? "" : isGroup ? S.me?.groups?.includes(id) ? " me" : "" : id === S.me?.id ? " me" : "");
            span.textContent = (isAI ? "🤖 @" : "@") + (isAI ? S.aiAccounts.find(a => a.id === id)?.name || mention[3] || id : isGroup ? S.groups?.get(id) || mention[3] || id : S.directory?.get(id) || mention[3] || id);
            span.title = "@" + (isAI ? "ai:" : isGroup ? "group:" : "") + id;
            if (editable) {
                span.contentEditable = "false";
                span.dataset.mentionToken = mention[0];
            }
            root.append(span);
            i += mention[0].length;
            continue;
        }
        let formatted = false;
        for (const [mark, tag] of markers) {
            if (!text.startsWith(mark, i)) continue;
            let end = i + mark.length;
            while ((end = text.indexOf(mark, end)) >= 0 && text[end - 1] === "\\") end += mark.length;
            if (end < 0 || end === i + mark.length) continue;
            flush();
            const element = document.createElement(tag);
            appendInlineMarkup(element, text.slice(i + mark.length, end), editable);
            root.append(element);
            i = end + mark.length;
            formatted = true;
            break;
        }
        if (!formatted) {
            plain += text[i];
            i++;
        }
    }
    flush();
}

function fragment(text, editable = false) {
    const root = document.createDocumentFragment(), lines = text.split("\n");
    let quote = null;
    lines.forEach((line, index) => {
        const quoted = line.startsWith("> ") || line === ">";
        if (quoted) {
            if (!quote) {
                quote = document.createElement("blockquote");
                root.append(quote);
            } else quote.append(document.createElement("br"));
            appendInlineMarkup(quote, line === ">" ? "" : line.slice(2), editable);
        } else {
            quote = null;
            appendInlineMarkup(root, line, editable);
        }
        if (index < lines.length - 1 && !quoted) root.append(document.createElement("br"));
    });
    return root;
}

function sanitizeMessageHTML(html) {
    const parser = new DOMParser();
    const doc = parser.parseFromString(html, "text/html");
    const allowedTags = new Set([
        "H1", "H2", "H3", "H4", "H5", "H6",
        "P", "BR", "HR", "BLOCKQUOTE",
        "PRE", "CODE",
        "UL", "OL", "LI",
        "TABLE", "THEAD", "TBODY", "TR", "TH", "TD",
        "STRONG", "B", "EM", "I", "U", "S", "DEL",
        "A", "SPAN", "DIV"
    ]);
    const allowedAttrs = new Set(["href", "title", "target", "rel", "class"]);

    function clean(node) {
        if (node.nodeType === Node.ELEMENT_NODE) {
            const tag = node.tagName.toUpperCase();
            if (!allowedTags.has(tag)) {
                if (["SCRIPT", "STYLE", "IFRAME", "OBJECT", "EMBED"].includes(tag)) {
                    node.remove();
                    return;
                }
                const parent = node.parentNode;
                if (parent) {
                    while (node.firstChild) {
                        parent.insertBefore(node.firstChild, node);
                    }
                    parent.removeChild(node);
                }
                return;
            }
            const attrs = Array.from(node.attributes);
            for (const a of attrs) {
                const name = a.name.toLowerCase();
                if (!allowedAttrs.has(name) && !name.startsWith("data-")) {
                    node.removeAttribute(a.name);
                    continue;
                }
                if (name === "href") {
                    const cleanHref = safeLinkURL(a.value);
                    if (!cleanHref) {
                        node.removeAttribute(a.name);
                    } else {
                        node.setAttribute("href", cleanHref);
                    }
                }
                if (name.startsWith("on")) {
                    node.removeAttribute(a.name);
                }
            }
            const children = Array.from(node.childNodes);
            for (const child of children) {
                clean(child);
            }
        }
    }
    const rootNodes = Array.from(doc.body.childNodes);
    for (const n of rootNodes) clean(n);
    return doc.body.innerHTML;
}

function processMarkdownDOM(container) {
    const codeBlocks = container.querySelectorAll("pre > code");
    codeBlocks.forEach(codeElem => {
        const pre = codeElem.parentElement;
        if (!pre || pre.parentElement?.classList.contains("code-block-wrapper")) return;

        const wrapper = document.createElement("div");
        wrapper.className = "code-block-wrapper";

        const header = document.createElement("div");
        header.className = "code-block-header";

        let langName = "CODE";
        const langClass = Array.from(codeElem.classList).find(c => c.startsWith("language-"));
        if (langClass) {
            langName = langClass.replace("language-", "").toUpperCase();
        }
        const langBadge = document.createElement("span");
        langBadge.className = "code-lang";
        langBadge.textContent = langName;

        const copyBtn = document.createElement("button");
        copyBtn.type = "button";
        copyBtn.className = "code-copy-btn";
        copyBtn.textContent = "コピー";
        copyBtn.onclick = async () => {
            const textToCopy = codeElem.textContent;
            let ok = false;
            try {
                if (navigator.clipboard?.writeText) {
                    await navigator.clipboard.writeText(textToCopy);
                    ok = true;
                }
            } catch (_) {}
            if (!ok) ok = fallbackCopyText(textToCopy);
            if (ok) {
                copyBtn.classList.add("copied");
                copyBtn.textContent = "コピー完了 ✓";
                setTimeout(() => {
                    copyBtn.classList.remove("copied");
                    copyBtn.textContent = "コピー";
                }, 2000);
            }
        };

        header.append(langBadge, copyBtn);
        pre.parentNode.insertBefore(wrapper, pre);
        wrapper.append(header, pre);
    });

    const tables = container.querySelectorAll("table");
    tables.forEach(table => {
        if (table.parentElement?.classList.contains("table-responsive")) return;
        const resp = document.createElement("div");
        resp.className = "table-responsive";
        table.parentNode.insertBefore(resp, table);
        resp.append(table);
    });

    const links = container.querySelectorAll("a");
    links.forEach(a => {
        const href = a.getAttribute("href") || "";
        const location = networkLocation(a.textContent, href);
        if (location) {
            a.replaceWith(location);
            return;
        }
        a.target = "_blank";
        a.rel = "noopener noreferrer";
        a.title = `リンク先: ${linkDestinationLabel(href)}`;
    });

    const mentionRegex = /@(?:(group|ai):)?([A-Za-z0-9][A-Za-z0-9_-]{0,63})(?:（([^\n）]*)）)?/g;
    function replaceMentionsInTextNodes(node) {
        if (node.nodeType === Node.TEXT_NODE) {
            const text = node.nodeValue;
            if (!text || !text.includes("@")) return;
            let match;
            mentionRegex.lastIndex = 0;
            let lastIndex = 0;
            const frag = document.createDocumentFragment();
            let hasMatch = false;

            while ((match = mentionRegex.exec(text)) !== null) {
                hasMatch = true;
                if (match.index > lastIndex) {
                    frag.append(document.createTextNode(text.slice(lastIndex, match.index)));
                }
                const kind = match[1];
                const id = match[2];
                const label = match[3];
                const isGroup = kind === "group";
                const isAI = kind === "ai";
                const span = document.createElement("span");
                span.className = "mention" + (isAI ? "" : isGroup ? S.me?.groups?.includes(id) ? " me" : "" : id === S.me?.id ? " me" : "");
                span.textContent = (isAI ? "🤖 @" : "@") + (isAI ? S.aiAccounts?.find(a => a.id === id)?.name || label || id : isGroup ? S.groups?.get(id) || label || id : S.directory?.get(id) || label || id);
                span.title = "@" + (isAI ? "ai:" : isGroup ? "group:" : "") + id;
                frag.append(span);
                lastIndex = mentionRegex.lastIndex;
            }
            if (hasMatch) {
                if (lastIndex < text.length) {
                    frag.append(document.createTextNode(text.slice(lastIndex)));
                }
                node.replaceWith(frag);
            }
            return;
        }
        if (node.nodeType === Node.ELEMENT_NODE) {
            const tag = node.tagName.toUpperCase();
            if (tag === "PRE" || tag === "CODE" || tag === "A" || node.classList.contains("mention") || node.classList.contains("code-block-header")) {
                return;
            }
            const children = Array.from(node.childNodes);
            for (const child of children) {
                replaceMentionsInTextNodes(child);
            }
        }
    }
    replaceMentionsInTextNodes(container);
}

function renderMessageMarkdown(text) {
    if (!text) {
        return document.createElement("span");
    }
    if (typeof marked === "undefined" || !marked.parse) {
        return fragment(text, false);
    }
    try {
        const rawHTML = marked.parse(text, { gfm: true, breaks: true });
        const cleanHTML = sanitizeMessageHTML(rawHTML);
        const container = document.createElement("div");
        container.className = "markdown-body";
        container.innerHTML = cleanHTML;
        processMarkdownDOM(container);
        return container;
    } catch (_) {
        return fragment(text, false);
    }
}
window.renderMessageMarkdown = renderMessageMarkdown;

function renderMessageText(message) {
    if (message.withdrawnAt) return withdrawnNotice(message);
    return renderMessageMarkdown(messageDisplayText(message));
}

function serializeEditorNode(node) {
    if (node.nodeType === Node.TEXT_NODE) return node.nodeValue || "";
    if (node.nodeType !== Node.ELEMENT_NODE) return "";
    if (node.dataset?.mentionToken) return node.dataset.mentionToken;
    const children = serializeEditorChildren(node);
    switch (node.tagName) {
    case "BR": return "\n";
    case "B": case "STRONG": return `**${children}**`;
    case "U": return children;
    case "S": case "STRIKE": return `~~${children}~~`;
    case "A": {
        const href = safeLinkURL(node.getAttribute("href") || "");
        return href ? `[${children}](${href.replace(/[()\\]/g, "\\$&")})` : children;
    }
    case "BLOCKQUOTE": return children.replace(/\n+$/, "").split("\n").map((line) => `> ${line}`).join("\n");
    case "DIV": case "P": return children;
    default: return children;
    }
}

function serializeEditorChildren(parent) {
    let value = "";
    for (const node of parent.childNodes) {
        const block = node.nodeType === Node.ELEMENT_NODE &&
            ["DIV", "P", "BLOCKQUOTE"].includes(node.tagName);
        if (block && value && !value.endsWith("\n")) value += "\n";
        value += serializeEditorNode(node);
    }
    return value;
}

function editorValue(root = editor) {
    return serializeEditorChildren(root).replace(/\n+$/, "");
}

function editorPlainText() {
    return editor.innerText.replace(/\n+$/, "");
}

function setEditorEnabled(enabled, placeholder = "") {
    editor.disabled = !enabled;
    editor.setAttribute("contenteditable", String(enabled));
    editor.setAttribute("aria-disabled", String(!enabled));
    if (placeholder) editor.dataset.placeholder = placeholder;
    syncEditorState();
}

Object.defineProperties(editor, {
    value: {
        get: editorValue,
        set(value) { editor.replaceChildren(fragment(value || "", true)); syncEditorState(); },
    },
    disabled: {
        get() { return editor.dataset.disabled !== "false"; },
        set(disabled) {
            editor.dataset.disabled = String(Boolean(disabled));
            editor.setAttribute("contenteditable", String(!disabled));
            editor.setAttribute("aria-disabled", String(Boolean(disabled)));
            syncEditorState();
        },
    },
    placeholder: {
        get() { return editor.dataset.placeholder || ""; },
        set(value) { editor.dataset.placeholder = value; },
    },
});

function syncEditorState() {
    const text = editorValue();
    const bytes = textEncoder.encode(text).length,
        over = bytes > MAX_MESSAGE_BYTES;
    editor.setAttribute("aria-invalid", String(over));

    const aiCount = countAIMentions(text);
    const hasAI = aiCount > 0;
    const multiAI = aiCount > 1;
    const hasAttachments = (S.stagedAttachments || []).length > 0;
    const attachWithoutAI = hasAttachments && !hasAI;

    const limitEl = $("editorLimit");
    if (limitEl) {
        if (over) {
            limitEl.textContent = `${bytes.toLocaleString()} / ${MAX_MESSAGE_BYTES.toLocaleString()} bytes`;
            limitEl.title = "";
        } else if (multiAI) {
            limitEl.textContent = "AIへのメンションは1件まで";
            limitEl.title = "AIへのメンションは1メッセージにつき1件のみ指定できます";
        } else if (attachWithoutAI) {
            limitEl.textContent = "添付はAI宛てのみ可能";
            limitEl.title = "添付ファイルはAI宛てメッセージ（@ai:...）にのみ送信できます";
        } else {
            limitEl.textContent = "";
            limitEl.title = "";
        }
    }

    const attachBtn = $("attachButton");
    if (attachBtn) {
        attachBtn.disabled = editor.disabled || !hasAI || multiAI;
        attachBtn.title = multiAI ? "AIへのメンションは1メッセージにつき1件のみ指定できます" : hasAI ? "ファイルを添付" : "ファイルを添付 (@ai:メンション時のみ有効)";
    }

    const inputHint = $("inputHint");
    if (inputHint) {
        inputHint.textContent = "";
        inputHint.classList.add("hidden");
    }

    const sendBtn = $("send");
    sendBtn.disabled = editor.disabled || over || attachWithoutAI || multiAI;
    if (multiAI) {
        sendBtn.title = "AIへのメンションは1メッセージにつき1件のみ指定できます";
    } else if (attachWithoutAI) {
        sendBtn.title = "添付ファイルはAI宛てメッセージ（@ai:...）にのみ送信できます";
    } else if (over) {
        sendBtn.title = "メッセージサイズが上限を超えています";
    } else {
        sendBtn.title = "送信（Ctrl+Enter）／Enterで改行";
    }

    for (const button of document.querySelectorAll(".composer .tools [data-format], #linkButton, #mentionButton, #aiButton"))
        button.disabled = editor.disabled;
}

function updateFormatButtons(root = editor, toolbar = document.querySelector(".composer .tools")) {
    const selection = window.getSelection();
    if (!selection.rangeCount || !root.contains(selection.getRangeAt(0).commonAncestorContainer)) return;
    for (const button of toolbar.querySelectorAll("[data-format]")) {
        const command = button.dataset.format;
        let active = false;
        try {
            active = command === "quote"
                ? document.queryCommandValue("formatBlock").toLowerCase() === "blockquote"
                : document.queryCommandState(command);
        } catch (_) {}
        button.classList.toggle("active", active);
        button.setAttribute("aria-pressed", String(active));
    }
}

function applyEditorFormat(command, root = editor, toolbar = document.querySelector(".composer .tools"), sync = syncEditorState) {
    root.focus();
    if (command === "quote") {
        const active = document.queryCommandValue("formatBlock").toLowerCase() === "blockquote";
        document.execCommand("formatBlock", false, active ? "div" : "blockquote");
    } else document.execCommand(command, false);
    sync();
    updateFormatButtons(root, toolbar);
}

function currentRichEditorRange(root) {
    const selection = window.getSelection();
    if (!selection.rangeCount) return null;
    const range = selection.getRangeAt(0);
    return root.contains(range.commonAncestorContainer) ? range.cloneRange() : null;
}

function installRichEditor(root, {toolbar, limit, sendButton, sync, onInput}) {
    Object.defineProperties(root, {
        value: {
            configurable: true,
            get() { return editorValue(root); },
            set(value) { root.replaceChildren(fragment(value || "", true)); sync(); },
        },
        disabled: {
            configurable: true,
            get() { return root.dataset.disabled !== "false"; },
            set(disabled) {
                root.dataset.disabled = String(Boolean(disabled));
                root.setAttribute("contenteditable", String(!disabled));
                root.setAttribute("aria-disabled", String(Boolean(disabled)));
                sync();
            },
        },
        placeholder: {
            configurable: true,
            get() { return root.dataset.placeholder || ""; },
            set(value) { root.dataset.placeholder = value; },
        },
    });
    const update = () => {
        const bytes = textEncoder.encode(editorValue(root)).length,
            over = bytes > MAX_MESSAGE_BYTES;
        limit.textContent = over ? `${bytes.toLocaleString()} / ${MAX_MESSAGE_BYTES.toLocaleString()} bytes` : "";
        root.setAttribute("aria-invalid", String(over));
        sendButton.disabled = root.disabled || over;
        for (const button of toolbar.querySelectorAll("button"))
            if (button !== sendButton && button.id !== "threadAttachButton" && button.id !== "attachButton") button.disabled = root.disabled;
    };
    root.addEventListener("input", () => { update(); sync?.(); onInput?.(); });
    root.addEventListener("keyup", () => updateFormatButtons(root, toolbar));
    root.addEventListener("mouseup", () => updateFormatButtons(root, toolbar));
    root.addEventListener("paste", (event) => {
        event.preventDefault();
        document.execCommand("insertText", false, event.clipboardData.getData("text/plain"));
    });
    toolbar.addEventListener("mousedown", (event) => {
        if (event.target.closest("button")) event.preventDefault();
    });
    for (const button of toolbar.querySelectorAll("[data-format]"))
        button.onclick = () => applyEditorFormat(button.dataset.format, root, toolbar, sync);
    return update;
}

$("logout").onclick = async () => {
    try {
        window.resetAICanvas?.();
        await api("/api/auth/logout", { method: "POST" });
        location.replace(Minihub.url("/login"));
    } catch (error) {
        note("ログアウトできません: " + error.message, true);
    }
};
async function send() {
    let text = $("input").value.trim();
    if (!text) return;
    if (textEncoder.encode(text).length > MAX_MESSAGE_BYTES) {
        note("メッセージは16 KiB以内にしてください", true);
        return;
    }
    if (countAIMentions(text) > 1) {
        note("AIへのメンションは1メッセージにつき1件のみ指定できます", true);
        return;
    }
    const attachmentIds = (S.stagedAttachments || []).map((a) => a.id);
    if (attachmentIds.length && !textHasAIMention(text)) {
        note("添付ファイルはAI宛てメッセージ（@ai:...）にのみ送信できます", true);
        return;
    }
    const channelID = S.channel.id;
    try {
        const message = await api(
            `/api/channels/${channelID}/messages`,
            { method: "POST", body: JSON.stringify({ text, attachmentIds }) },
        );
		stopTyping();
        S.stagedAttachments = [];
        renderStagedAttachments();
        $("input").value = "";
        syncEditorState();
		advanceLocalRead(channelID, message.seq);
		api(`/api/channels/${encodeURIComponent(channelID)}/read`, {
			method: "PUT",
			body: JSON.stringify({ seq: message.seq }),
		}).catch((error) =>
			note(`メッセージは送信されましたが、既読状態を更新できません: ${error.message}`, true),
		);
        focusMessage(message.seq, channelID);
        catchup();
    } catch (x) {
        note(x.message, true);
    }
}
$("send").onclick = send;
$("input").onkeydown = (e) => {
    if (e.ctrlKey && e.key === "Enter") {
        e.preventDefault();
        send();
    }
    if (e.ctrlKey && e.key.toLowerCase() === "b") {
        e.preventDefault();
        applyEditorFormat("bold");
    }
};
document
    .querySelectorAll(".composer .tools [data-format]")
    .forEach((button) => (button.onclick = () => applyEditorFormat(button.dataset.format)));
document.querySelector(".tools").addEventListener("mousedown", (event) => {
    if (event.target.closest("button")) event.preventDefault();
});
editor.addEventListener("input", syncEditorState);
editor.addEventListener("keyup", () => updateFormatButtons());
editor.addEventListener("mouseup", () => updateFormatButtons());
editor.addEventListener("paste", (event) => {
    event.preventDefault();
    document.execCommand("insertText", false, event.clipboardData.getData("text/plain"));
});
$("attachButton").onclick = () => $("attachFileInput").click();
$("attachFileInput").onchange = async (event) => {
    const files = Array.from(event.target.files || []);
    event.target.value = "";
    if (!files.length) return;
    if (!S.channel) {
        note("チャンネルを選択してください", true);
        return;
    }
    if ((S.stagedAttachments.length + files.length) > 5) {
        note("添付できるファイルは最大5件までです", true);
        return;
    }
    for (const f of files) {
        if (!isAllowedAttachmentFile(f.name)) {
            note(`未対応のファイル形式です: ${f.name}（Office文書、PDF、テキスト/データ形式のみ対応）`, true);
            return;
        }
        if (f.size > 20 * 1024 * 1024) {
            note(`ファイルサイズが20MBを超えています: ${f.name}`, true);
            return;
        }
    }
    const currentTotal = S.stagedAttachments.reduce((sum, a) => sum + (a.size || 0), 0);
    const newTotal = files.reduce((sum, f) => sum + f.size, 0);
    if (currentTotal + newTotal > 50 * 1024 * 1024) {
        note("添付ファイルの合計サイズは50MB以下にしてください", true);
        return;
    }
    $("attachButton").disabled = true;
    try {
        for (const f of files) {
            const att = await uploadAttachment(S.channel.id, f);
            S.stagedAttachments.push(att);
        }
    } catch (err) {
        note(err.message, true);
    } finally {
        $("attachButton").disabled = false;
        renderStagedAttachments();
        syncEditorState();
    }
};
$("join").onclick = async () => {
    const channelID = S.channel?.id;
    if (!channelID || S.channelActionBusy) return;
    const action = directMember(S.channel) ? "leave" : "join";
    if (action === "leave" && !confirm(`「${S.channel.name || S.channel.id}」から退出しますか？ 退出後も公開チャンネルなので、いつでも再参加できます。`)) return;
    S.channelActionBusy = true;
    renderChannelAction();
    try {
        await api(`/api/channels/${encodeURIComponent(channelID)}/${action}`, {
            method: "POST",
        });
        if (S.channel?.id === channelID) await select(channelID);
    } catch (error) {
        note(error.message, true);
    } finally {
        if (S.channel?.id === channelID && S.channelActionBusy) {
            S.channelActionBusy = false;
            renderChannelAction();
        }
    }
};

$("leaveChannel").onclick = () => $("join").onclick();

// Notification center and self-service password change.
S.localNotifications = [];
S.mentions = [];
S.mentionCursor = "";
S.mentionUnread = 0;

function notificationStorageKey() {
    return Minihub.storageKey(`minihub.notifications.${S.me?.id || "anonymous"}`);
}
function loadLocalNotifications() {
    try {
        S.localNotifications = JSON.parse(localStorage.getItem(notificationStorageKey()) || "[]");
    } catch (_) {
        S.localNotifications = [];
    }
    renderNotificationCenter();
}
function saveLocalNotifications() {
    try { localStorage.setItem(notificationStorageKey(), JSON.stringify(S.localNotifications.slice(0, 100))); } catch (_) {}
}
function storeLocalNotification(message, type) {
    if (!S.me) return;
    S.localNotifications.unshift({ id: crypto.randomUUID?.() || `${Date.now()}-${Math.random()}`, message, type, ts: new Date().toISOString(), seen: false });
    S.localNotifications = S.localNotifications.slice(0, 100);
    saveLocalNotifications();
    renderNotificationCenter();
}
function updateNotificationBadge() {
    const count = S.mentionUnread + S.localNotifications.filter((item) => !item.seen).length,
        badge = $("notificationBadge");
    badge.classList.toggle("hidden", !count);
    badge.textContent = count > 99 ? "99+" : String(count || "");
    badge.setAttribute("aria-label", `未確認通知${count}件`);
}
function renderNotificationCenter() {
    const notices = $("noticeList");
    if (!notices) return;
    $("clearNotifications").disabled = !S.localNotifications.length;
    if ($("clearAllNotificationsBtn")) {
        $("clearAllNotificationsBtn").disabled = !S.localNotifications.length && !S.mentionUnread;
    }
    notices.replaceChildren();
    for (const item of S.localNotifications) {
        const row = document.createElement("article");
        row.className = `notification-item ${item.seen ? "" : "unseen"}`;
        row.innerHTML = '<div class="notification-copy"></div><time></time>';
        row.querySelector("div").textContent = item.message;
        row.querySelector("time").textContent = new Date(item.ts).toLocaleString("ja-JP");
        notices.append(row);
    }
    if (!S.localNotifications.length) notices.textContent = "通知はありません";
    renderMentions();
    updateNotificationBadge();
}
function renderMentions() {
    const host = $("mentionList");
    if (!host) return;
    host.replaceChildren();
    for (const mention of S.mentions) {
        const row = document.createElement("button");
        row.type = "button";
        row.className = `notification-item mention-notification ${mention.readAt ? "" : "unseen"}`;
        const channel = S.channels.find((item) => item.id === mention.channelId);
        row.innerHTML = '<b></b><span></span><time></time>';
        row.querySelector("b").textContent = `# ${channel?.name || mention.channelId}`;
        row.querySelector("span").textContent = messageDisplayText(mention.message);
        row.querySelector("time").textContent = new Date(mention.message.ts).toLocaleString("ja-JP");
        row.onclick = () => openMention(mention);
        host.append(row);
    }
    if (!S.mentions.length) host.textContent = "メンションはありません";
    $("moreMentions").classList.toggle("hidden", !S.mentionCursor || host.classList.contains("hidden"));
}
async function loadMentions(append = false) {
    try {
        const cursor = append && S.mentionCursor ? `&cursor=${encodeURIComponent(S.mentionCursor)}` : "",
            page = await api(`/api/mentions?limit=50${cursor}`);
        S.mentions = append ? S.mentions.concat(page.mentions) : page.mentions;
        S.mentionCursor = page.nextCursor || "";
        S.mentionUnread = page.unreadCount;
        renderNotificationCenter();
    } catch (error) {
        if (S.me) note(`メンションを取得できません: ${error.message}`, "warning");
    }
}
async function openMention(mention) {
    if (mention.message.threadRootSeq) { await openThreadMention(mention); return; }
    try {
        await select(mention.channelId);
        const page = await api(`/api/channels/${encodeURIComponent(mention.channelId)}/messages?around=${mention.message.seq}&limit=100`);
        if (S.channel?.id !== mention.channelId) return;
        S.msgs = page.messages;
        S.nextBefore = page.nextBefore;
        render();
        focusMessage(mention.message.seq, mention.channelId, true);
        if (!mention.readAt) {
            await api(`/api/mentions/${encodeURIComponent(mention.message.id)}/read`, { method: "PUT" });
            mention.readAt = new Date().toISOString();
            S.mentionUnread = Math.max(0, S.mentionUnread - 1);
            renderNotificationCenter();
        }
        bootstrap.Offcanvas.getOrCreateInstance($("notificationCenter")).hide();
    } catch (error) {
        note(`メンションを開けません: ${error.message}`, true);
    }
}

$("noticeTab").onclick = () => {
    $("noticeTab").classList.add("active"); $("mentionTab").classList.remove("active");
    $("noticeActions").classList.remove("hidden");
    $("noticeList").classList.remove("hidden"); $("mentionList").classList.add("hidden"); $("moreMentions").classList.add("hidden");
    for (const item of S.localNotifications) item.seen = true;
    saveLocalNotifications(); renderNotificationCenter();
};
$("mentionTab").onclick = () => {
    $("mentionTab").classList.add("active"); $("noticeTab").classList.remove("active");
    $("noticeActions").classList.add("hidden");
    $("mentionList").classList.remove("hidden"); $("noticeList").classList.add("hidden");
    $("moreMentions").classList.toggle("hidden", !S.mentionCursor);
};
$("clearNotifications").onclick = () => {
    if (!S.localNotifications.length || !window.confirm("通知をすべてクリアしますか？")) return;
    S.localNotifications = [];
    try { localStorage.removeItem(notificationStorageKey()); } catch (_) {}
    renderNotificationCenter();
};
if ($("clearAllNotificationsBtn")) {
    $("clearAllNotificationsBtn").onclick = async () => {
        if (!S.localNotifications.length && !S.mentionUnread) return;
        if (!window.confirm("すべての通知と未読メンションをクリアしますか？")) return;
        S.localNotifications = [];
        try { localStorage.removeItem(notificationStorageKey()); } catch (_) {}
        const unreadMentions = (S.mentions || []).filter((m) => !m.readAt);
        if (unreadMentions.length) {
            await Promise.allSettled(
                unreadMentions.map((m) => api(`/api/mentions/${encodeURIComponent(m.message.id)}/read`, { method: "PUT" }))
            );
            for (const m of unreadMentions) m.readAt = new Date().toISOString();
            S.mentionUnread = 0;
        }
        renderNotificationCenter();
        note("すべての通知をクリアしました", "success", false);
    };
}
$("moreMentions").onclick = () => loadMentions(true);
$("notificationCenter").addEventListener("show.bs.offcanvas", () => {
    closeMobileChannelNav("none");
    $("mentionTab").click();
    loadMentions();
});

$("changePassword").onclick = () => bootstrap.Modal.getOrCreateInstance($("passwordModal")).show();
$("passwordForm").onsubmit = async (event) => {
    event.preventDefault();
    const currentPassword = $("currentPassword").value,
        newPassword = $("newPassword").value;
    if (newPassword !== $("confirmPassword").value) { note("新しいパスワードが一致しません", true); return; }
    try {
        await api("/api/me/password", { method: "PUT", body: JSON.stringify({ currentPassword, newPassword }) });
        event.target.reset();
        bootstrap.Modal.getOrCreateInstance($("passwordModal")).hide();
        note("パスワードを変更しました。ほかの端末はログアウトされました");
    } catch (error) { note(error.message, true); }
};

api("/api/me")
    .then(async (me) => {
        setup(me);
		await loadMentions();
        await loadChannels();
        realtime();
        if (typeof refreshParticipatingThreads === "function") refreshParticipatingThreads();
    })
    .catch(() => {});
// UI state enhancements: persisted unread state and serialized message catch-up.
S.read = new Map();
S.directory = new Map();
S.presence = new Map();
S.presenceRefs = new Map();
S.mainMentionChannel = null;
S.reactions = new Map();
S.reactionUsers = new Map();
S.typers = new Map();
S.typingScope = null;
S.typingHeartbeat = null;
const reactionOptions = [
    ["ack", "hand-thumbs-up", "了解"],
    ["done", "check-circle", "完了"],
    ["eyes", "hourglass-split", "確認中"],
    ["thanks", "heart", "感謝"],
];

function sendRealtime(event) {
    if (S.ws?.readyState !== WebSocket.OPEN) return false;
    try {
        S.ws.send(JSON.stringify(event));
        return true;
    } catch (_) {
        return false;
    }
}

function stopTyping(send = true) {
    if (S.typingHeartbeat) clearInterval(S.typingHeartbeat);
    S.typingHeartbeat = null;
    const scope = S.typingScope;
    S.typingScope = null;
    if (send && scope)
        sendRealtime({ type: "typing_stop", channelId: scope.channelID, threadRootSeq: scope.threadRootSeq });
}

function updateOwnTyping() {
    const mainInput = $("input"), threadInput = $("threadInput");
    const thread = typeof T === "undefined" ? null : T.current;
    let scope = null;
    if (thread && !threadInput.disabled && document.activeElement === threadInput && threadInput.value.trim())
        scope = {channelID: thread.channel, threadRootSeq: thread.root};
    else if (S.channel?.id && !mainInput.disabled && document.activeElement === mainInput && mainInput.value.trim())
        scope = {channelID: S.channel.id, threadRootSeq: 0};
    if (!scope) {
        stopTyping();
        return;
    }
    if (S.typingScope?.channelID === scope.channelID && S.typingScope?.threadRootSeq === scope.threadRootSeq) return;
    stopTyping();
    if (!sendRealtime({ type: "typing_start", channelId: scope.channelID, threadRootSeq: scope.threadRootSeq })) return;
    S.typingScope = scope;
    S.typingHeartbeat = setInterval(() => {
        if (!sendRealtime({ type: "typing_start", channelId: scope.channelID, threadRootSeq: scope.threadRootSeq }))
            stopTyping(false);
    }, 2000);
}

function typingNames(channelID, threadRootSeq) {
    return [...new Set(
        [...S.typers.values()]
            .filter((entry) => entry.channelID === channelID && entry.threadRootSeq === threadRootSeq)
            .map((entry) => entry.userID),
    )].map((id) => S.directory.get(id) || id);
}

function setTypingStatus(status, names) {
    if (!status) return;
    if (!names.length) status.textContent = "";
    else if (names.length === 1) status.textContent = `${names[0]}さんが入力中です…`;
    else if (names.length === 2) status.textContent = `${names[0]}さん、${names[1]}さんが入力中です…`;
    else status.textContent = `${names[0]}さん、${names[1]}さんほか${names.length - 2}人が入力中です…`;
}

function renderTypingStatus() {
    const now = Date.now(),
        channelID = S.channel?.id;
    for (const [id, entry] of S.typers)
        if (entry.expiresAt <= now) S.typers.delete(id);
    setTypingStatus($("typingStatus"), typingNames(channelID, 0));
    const thread = typeof T === "undefined" ? null : T.current;
    setTypingStatus($("threadTypingStatus"), thread ? typingNames(thread.channel, thread.root) : []);
}

function handleTypingEvent(message) {
    if (!message.userId || message.userId === S.me?.id) return;
    const key = message.typingSession || message.userId;
    if (message.type === "typing_stop") S.typers.delete(key);
    else
        S.typers.set(key, {
            userID: message.userId,
            channelID: message.channelId,
            threadRootSeq: message.threadRootSeq || 0,
            expiresAt: Date.now() + 5500,
        });
    renderTypingStatus();
}

function advanceLocalRead(channelID, seq) {
    const current = S.read.get(channelID) || {};
    const status = {...current, lastReadSeq: Math.max(current.lastReadSeq || 0, seq)};
    // A newer server status can race with the send response; counts come from the API.
    if ((current.latestSeq || 0) <= seq) { status.latestSeq = seq; status.unreadCount = 0; }
    S.read.set(channelID, status);
    S.unread.set(channelID, status.unreadCount || 0);
}

setInterval(renderTypingStatus, 1000);
$("input").addEventListener("input", updateOwnTyping);
$("input").addEventListener("focus", updateOwnTyping);
$("input").addEventListener("blur", () => stopTyping());
document.addEventListener("visibilitychange", () => {
    if (document.hidden) stopTyping();
    else updateOwnTyping();
});

function applyReactionView(view) {
    if (!view) return;
    if (view.reactions?.length) S.reactions.set(view.messageSeq, view.reactions);
    else S.reactions.delete(view.messageSeq);
}

async function syncReactions(channelID, afterMessageSeq = 0, replace = true) {
    const views = await api(
        `/api/channels/${encodeURIComponent(channelID)}/reactions?afterMessageSeq=${afterMessageSeq}`,
    );
    if (S.channel?.id !== channelID) return;
    if (replace) S.reactions.clear();
    for (const view of views) applyReactionView(view);
    render();
}

async function toggleReaction(messageSeq, key, active) {
    if (!S.channel || !member(S.channel)) return;
    invalidateReactionUsers(S.channel.id, messageSeq);
    try {
        const view = await api(
            `/api/channels/${encodeURIComponent(S.channel.id)}/messages/${messageSeq}/reactions/${key}`,
            { method: active ? "PUT" : "DELETE" },
        );
        applyReactionView(view);
        render();
        if (typeof refreshThreadReaction === "function") refreshThreadReaction(messageSeq);
    } catch (error) {
        note(error.message, true);
    }
}

function setReactionPickerOpen(picker, open) {
    picker.classList.toggle("open", open);
    picker.previousElementSibling?.setAttribute("aria-expanded", String(open));
}

function closeReactionPickers(except = null) {
    for (const picker of document.querySelectorAll(".reaction-picker.open")) {
        if (picker !== except) setReactionPickerOpen(picker, false);
    }
}

document.addEventListener("click", () => closeReactionPickers());

function reactionUsersCacheKey(channelID, messageSeq, key) {
    return `${channelID}\u0000${messageSeq}\u0000${key}`;
}

function invalidateReactionUsers(channelID, messageSeq) {
    const prefix = `${channelID}\u0000${messageSeq}\u0000`;
    for (const key of S.reactionUsers.keys()) {
        if (key.startsWith(prefix)) S.reactionUsers.delete(key);
    }
}

async function showReactionUsers(messageSeq, key, label, tooltip) {
    if (!S.channel || tooltip.dataset.loaded === "true") return;
    tooltip.dataset.loaded = "true";
    tooltip.textContent = "読み込み中…";
    const channelID = S.channel.id,
        cacheKey = reactionUsersCacheKey(channelID, messageSeq, key);
    let pending = S.reactionUsers.get(cacheKey);
    if (!pending) {
        pending = api(
            `/api/channels/${encodeURIComponent(channelID)}/messages/${messageSeq}/reactions/${encodeURIComponent(key)}/users`,
        );
        S.reactionUsers.set(cacheKey, pending);
    }
    try {
        const response = await pending;
        if (!tooltip.isConnected || S.channel?.id !== channelID) return;
        const userIDs = response.userIds || [];
        tooltip.replaceChildren();
        const heading = document.createElement("b");
        heading.textContent = `${label} · ${userIDs.length}人`;
        tooltip.append(heading);
        if (!userIDs.length) {
            const empty = document.createElement("span");
            empty.textContent = "リアクションはありません";
            tooltip.append(empty);
            return;
        }
        const list = document.createElement("span");
        list.className = "reaction-users-list";
        for (const userID of userIDs) {
            const name = document.createElement("span");
            name.textContent = S.directory.get(userID) || `@${userID}`;
            list.append(name);
        }
        tooltip.append(list);
    } catch (_) {
        S.reactionUsers.delete(cacheKey);
        if (!tooltip.isConnected) return;
        tooltip.dataset.loaded = "false";
        tooltip.textContent = "参加者を取得できませんでした";
    }
}

function reactionBar(messageSeq, allowAdd) {
    const bar = document.createElement("div");
    bar.className = "reactions message-actions";
    bar.setAttribute("role", "group");
    bar.setAttribute("aria-label", "メッセージの操作");
    const current = new Map(
        (S.reactions.get(messageSeq) || []).map((item) => [item.key, item]),
    );
    for (const [key, iconName, label] of reactionOptions) {
        const item = current.get(key);
        if (!item) continue;
        const button = document.createElement("button");
        button.type = "button";
        button.className = "reaction-chip" + (item.reactedByMe ? " mine" : "");
        button.dataset.reaction = key;
        button.innerHTML = `${icon(iconName)}<span>${item.count}</span>`;
        button.setAttribute("aria-label", `${label} ${item.count}件${item.reactedByMe ? "、取り消す" : ""}`);
        button.title = `${label}${item.reactedByMe ? "を取り消す" : ""}`;
        button.disabled = !member(S.channel);
        button.onclick = () => toggleReaction(messageSeq, key, !item.reactedByMe);
        const wrap = document.createElement("span"),
            tooltip = document.createElement("span"),
            tooltipID = `reaction-users-${messageSeq}-${key}`;
        wrap.className = "reaction-chip-wrap";
        tooltip.className = "reaction-users-tooltip";
        tooltip.id = tooltipID;
        tooltip.setAttribute("role", "tooltip");
        button.setAttribute("aria-describedby", tooltipID);
        const loadUsers = () => showReactionUsers(messageSeq, key, label, tooltip);
        wrap.addEventListener("mouseenter", loadUsers);
        button.addEventListener("focus", loadUsers);
        wrap.append(button, tooltip);
        bar.append(wrap);
    }
    if (member(S.channel) && allowAdd) {
        const wrap = document.createElement("span");
        wrap.className = "reaction-add";
        const add = document.createElement("button");
        add.type = "button";
        add.className = "reaction-add-button";
        add.innerHTML = icon("plus");
        add.title = "リアクションを追加";
        add.setAttribute("aria-label", "リアクションを追加");
        add.setAttribute("aria-expanded", "false");
        const picker = document.createElement("span");
        picker.className = "reaction-picker";
        for (const [key, iconName, label] of reactionOptions) {
            const option = document.createElement("button");
            option.type = "button";
            option.dataset.reaction = key;
            option.innerHTML = icon(iconName);
            option.title = label;
            option.setAttribute("aria-label", label);
            option.onclick = () => {
                setReactionPickerOpen(picker, false);
                toggleReaction(messageSeq, key, true);
            };
            picker.append(option);
        }
        add.onclick = (event) => {
            event.stopPropagation();
            const open = !picker.classList.contains("open");
            closeReactionPickers(picker);
            setReactionPickerOpen(picker, open);
        };
        wrap.append(add, picker);
        bar.append(wrap);
    }
    return bar;
}
S.catching = false;
S.pending = false;
S.nextBefore = null;
S.loadingOlder = false;
S.historyError = "";
S.selectionToken = 0;
S.initialLoading = false;
S.initialLoadingVisible = false;
S.initialError = "";
S.olderLoadingVisible = false;
S.catchupLoadingVisible = false;
S.catchupError = "";
S.loadingTimers = {};
S.markingRead = false;
S.channelActionBusy = false;
S.highlightedMessage = null;
S.messageHighlightTimer = null;

function cancelLoadingTimer(kind) {
    clearTimeout(S.loadingTimers[kind]);
    delete S.loadingTimers[kind];
}

function showLoadingAfterDelay(kind, valid, show) {
    cancelLoadingTimer(kind);
    S.loadingTimers[kind] = setTimeout(() => {
        delete S.loadingTimers[kind];
        if (!valid()) return;
        show();
        render();
    }, 150);
}

function updateHistoryBusy() {
    $("messages").setAttribute(
        "aria-busy",
        String(S.initialLoading || S.loadingOlder || S.catching),
    );
}

function historyStatus(label, className = "", retry) {
    const status = document.createElement("div");
    status.className = `history-status ${className}`.trim();
    status.setAttribute("role", retry ? "alert" : "status");
    status.setAttribute("aria-live", "polite");
    if (!retry) {
        const spinner = document.createElement("span");
        spinner.className = "spinner-border spinner-border-sm";
        spinner.setAttribute("aria-hidden", "true");
        status.append(spinner, document.createTextNode(label));
        return status;
    }
    const message = document.createElement("span"),
        button = document.createElement("button");
    message.textContent = label;
    button.type = "button";
    button.className = "btn btn-outline-secondary btn-sm";
    button.textContent = "再読み込み";
    button.onclick = retry;
    status.append(message, button);
    return status;
}
const desktopChannelInfo = window.matchMedia("(min-width: 981px)"),
    appShell = document.querySelector(".app"),
    channelInfoPanel = $("channelInfoPanel"),
    channelInfoToggle = $("channelInfoToggle");
const mobileChannelNav = window.matchMedia("(max-width: 720px)"),
    channelNav = $("channelNav"),
    channelNavOpen = $("channelNavOpen");
S.channelInfoCollapsed = false;
S.channelInfoReturnFocus = false;

function syncMobileChannelNav() {
    const open = mobileChannelNav.matches && appShell.classList.contains("channel-nav-open"),
        threadOverlay = typeof T !== "undefined" && !!T.current && window.matchMedia("(max-width: 1050px)").matches;
    channelNav.inert = (mobileChannelNav.matches && !open) || threadOverlay;
    document.querySelector(".main").inert = open || threadOverlay;
    channelInfoPanel.inert = open;
    channelNavOpen.setAttribute("aria-expanded", String(open));
    if (mobileChannelNav.matches) {
        channelNav.setAttribute("aria-hidden", String(!open));
        if (open) {
            channelNav.setAttribute("role", "dialog");
            channelNav.setAttribute("aria-modal", "true");
            channelNav.setAttribute("aria-labelledby", "channelNavTitle");
        } else {
            channelNav.removeAttribute("role");
            channelNav.removeAttribute("aria-modal");
            channelNav.removeAttribute("aria-labelledby");
        }
    } else {
        channelNav.removeAttribute("aria-hidden");
        channelNav.removeAttribute("role");
        channelNav.removeAttribute("aria-modal");
        channelNav.removeAttribute("aria-labelledby");
    }
}

function closeMobileChannelNav(focus = "opener") {
    if (!appShell.classList.contains("channel-nav-open")) return;
    appShell.classList.remove("channel-nav-open");
    syncMobileChannelNav();
    if (mobileChannelNav.matches) {
        if (focus === "title") $("title").focus();
        else if (focus === "opener") channelNavOpen.focus();
    }
}

channelNavOpen.onclick = () => {
    appShell.classList.add("channel-nav-open");
    syncMobileChannelNav();
    $("channelNavClose").focus();
};
$("channelNavClose").onclick = () => closeMobileChannelNav();
$("channelNavBackdrop").onclick = () => closeMobileChannelNav();
document.addEventListener("show.bs.modal", () => closeMobileChannelNav("none"));
document.addEventListener("keydown", (event) => {
    if (!mobileChannelNav.matches || !appShell.classList.contains("channel-nav-open")) return;
    if (event.key === "Escape") {
        event.preventDefault();
        closeMobileChannelNav();
    } else if (event.key === "Tab") {
        const focusable = Array.from(channelNav.querySelectorAll("button:not(:disabled), a[href], input:not(:disabled), select:not(:disabled)"))
            .filter((element) => element.getClientRects().length);
        if (!focusable.length) return;
        const first = focusable[0], last = focusable.at(-1);
        if (event.shiftKey && document.activeElement === first) {
            event.preventDefault();
            last.focus();
        } else if (!event.shiftKey && document.activeElement === last) {
            event.preventDefault();
            first.focus();
        }
    }
});
mobileChannelNav.addEventListener("change", () => {
    const focusWasInNav = channelNav.contains(document.activeElement) || document.activeElement === document.body,
        wasOpen = appShell.classList.contains("channel-nav-open");
    appShell.classList.remove("channel-nav-open");
    syncMobileChannelNav();
    if (mobileChannelNav.matches && focusWasInNav)
        setTimeout(() => {
            if (mobileChannelNav.matches && !appShell.classList.contains("channel-nav-open")) channelNavOpen.focus();
        }, 0);
    else if (!mobileChannelNav.matches && wasOpen)
        setTimeout(() => {
            if (!mobileChannelNav.matches)
                (channelNav.querySelector(".channel[aria-current]") || channelNav.querySelector(".channel") || channelNav).focus();
        }, 0);
});
syncMobileChannelNav();

function uiStorageKey(name) {
    return Minihub.storageKey(`minihub:ui:${S.me?.id || "anonymous"}:${name}`);
}

function readUIPreference(name) {
    try {
        return localStorage.getItem(uiStorageKey(name)) === "1";
    } catch (_) {
        return false;
    }
}

function saveUIPreference(name, collapsed) {
    if (!S.me) return;
    try {
        localStorage.setItem(uiStorageKey(name), collapsed ? "1" : "0");
    } catch (_) {}
}

function updateChannelInfoToggle(open) {
    const label = `チャンネル情報を${open ? "閉じる" : "開く"}`;
    channelInfoToggle.setAttribute("aria-expanded", String(open));
    channelInfoToggle.setAttribute("aria-label", label);
    channelInfoToggle.title = label;
}

function setDesktopChannelInfoCollapsed(collapsed, persist = true) {
    S.channelInfoCollapsed = collapsed;
    if (desktopChannelInfo.matches) {
        appShell.classList.toggle("channel-info-collapsed", collapsed);
        updateChannelInfoToggle(!collapsed);
    }
    if (persist) saveUIPreference("channel-info-collapsed", collapsed);
}

function setAdminMenuCollapsed(collapsed, persist = true) {
    $("admin").classList.toggle("collapsed", collapsed);
    $("adminActions").classList.toggle("hidden", collapsed);
    $("adminToggle").setAttribute("aria-expanded", String(!collapsed));
    $("adminToggle").title = `管理者メニューを${collapsed ? "開く" : "閉じる"}`;
    if (persist) saveUIPreference("admin-menu-collapsed", collapsed);
}

function setChannelSectionCollapsed(type, collapsed, persist = true) {
    const toggle = $(`${type}Toggle`),
        list = $(type),
        section = toggle.closest(".channel-section");
    section.classList.toggle("collapsed", collapsed);
    list.classList.toggle("hidden", collapsed);
    toggle.setAttribute("aria-expanded", String(!collapsed));
    toggle.title = `${type === "public" ? "公開" : "非公開"}チャンネルを${collapsed ? "開く" : "閉じる"}`;
    if (persist) saveUIPreference(`${type}-channels-collapsed`, collapsed);
}

function updateChannelSectionSummary(type, channels) {
    const unread = channels.reduce(
            (total, channel) => total + (member(channel) ? S.unread.get(channel.id) || 0 : 0),
            0,
        ),
        badge = $(`${type}Unread`),
        section = $(`${type}Toggle`).closest(".channel-section");
    $(`${type}Count`).textContent = channels.length;
    badge.textContent = unread > 99 ? "99+" : unread;
    badge.setAttribute("aria-label", `未読${unread}件`);
    badge.classList.toggle("hidden", unread === 0);
    section.classList.toggle(
        "contains-active",
        channels.some((channel) => channel.id === S.channel?.id),
    );
}

function filterChannelList() {
    const normalize = (value) => value.normalize("NFKC").toLocaleLowerCase("ja"),
        query = normalize($("channelSearch").value.trim()),
        searching = query.length > 0,
        wasSearching = appShell.classList.contains("channel-search-active");
    appShell.classList.toggle("channel-search-active", searching);
    let matches = 0;
    for (const type of ["public", "private"]) {
        const host = $(type),
            rows = Array.from(host.querySelectorAll(".channel"));
        let visible = 0;
        for (const row of rows) {
            const match = !searching ||
                normalize(row.querySelector(".channel-name").textContent).includes(query) ||
                normalize(row.dataset.channelId).includes(query);
            row.classList.toggle("hidden", !match);
            if (match) visible++;
        }
        matches += visible;
        $(`${type}Count`).textContent = searching ? `${visible}/${rows.length}` : rows.length;
        let empty = host.querySelector(".channel-list-empty");
        if (!empty) {
            empty = document.createElement("div");
            empty.className = "channel-list-empty";
            host.append(empty);
        }
        empty.textContent = searching
            ? "一致するチャンネルはありません。別のキーワードをお試しください"
            : type === "public"
                ? "公開チャンネルはまだありません。管理者に作成を依頼してください"
                : "参加中の非公開チャンネルはありません。招待されるとここに表示されます";
        empty.classList.toggle("hidden", visible > 0);
        if (searching || wasSearching)
            setChannelSectionCollapsed(type, searching ? false : readUIPreference(`${type}-channels-collapsed`), false);
    }
    $("channelSearchStatus").textContent = searching ? `検索結果: ${matches}件` : "";
}

$("channelSearch").addEventListener("input", filterChannelList);
$("channelSearch").addEventListener("keydown", (event) => {
    if (event.key === "Escape" && !event.isComposing && event.target.value) {
        event.preventDefault();
        event.stopPropagation();
        event.target.value = "";
        filterChannelList();
    }
});

function syncChannelInfoViewport() {
    if (desktopChannelInfo.matches) {
        S.channelInfoReturnFocus = false;
        bootstrap.Offcanvas.getInstance(channelInfoPanel)?.hide();
        appShell.classList.toggle(
            "channel-info-collapsed",
            S.channelInfoCollapsed,
        );
        updateChannelInfoToggle(!S.channelInfoCollapsed);
        return;
    }
    appShell.classList.remove("channel-info-collapsed");
    updateChannelInfoToggle(channelInfoPanel.classList.contains("show"));
}

channelInfoToggle.onclick = () => {
    if (desktopChannelInfo.matches) {
        setDesktopChannelInfoCollapsed(!S.channelInfoCollapsed);
        return;
    }
    S.channelInfoReturnFocus = true;
    bootstrap.Offcanvas.getOrCreateInstance(channelInfoPanel).show();
};
$("channelInfoClose").onclick = () => {
    if (desktopChannelInfo.matches) {
        setDesktopChannelInfoCollapsed(true);
        return;
    }
    bootstrap.Offcanvas.getOrCreateInstance(channelInfoPanel).hide();
};
$("adminToggle").onclick = () =>
    setAdminMenuCollapsed(
        $("adminToggle").getAttribute("aria-expanded") === "true",
    );
for (const type of ["public", "private"])
    $(`${type}Toggle`).onclick = () =>
        setChannelSectionCollapsed(
            type,
            $(`${type}Toggle`).getAttribute("aria-expanded") === "true",
        );
channelInfoPanel.addEventListener("show.bs.offcanvas", () => {
    closeMobileChannelNav("none");
    updateChannelInfoToggle(true);
});
channelInfoPanel.addEventListener("hidden.bs.offcanvas", () => {
    updateChannelInfoToggle(
        desktopChannelInfo.matches ? !S.channelInfoCollapsed : false,
    );
    if (S.channelInfoReturnFocus && !desktopChannelInfo.matches)
        channelInfoToggle.focus();
    S.channelInfoReturnFocus = false;
});
desktopChannelInfo.addEventListener("change", syncChannelInfoViewport);

setup = function (me) {
    S.me = me;
    S.csrf = me.csrfToken;
    $("me").textContent = me.name;
    $("avatar").textContent = ini(me.name);
    $("admin").classList.toggle("hidden", me.role !== "admin");
    $("changePassword").classList.toggle(
        "hidden",
        !me.capabilities?.selfPasswordChange,
    );
    setDesktopChannelInfoCollapsed(
        readUIPreference("channel-info-collapsed"),
        false,
    );
    if (me.role === "admin")
        setAdminMenuCollapsed(
            readUIPreference("admin-menu-collapsed"),
            false,
        );
    for (const type of ["public", "private"])
        setChannelSectionCollapsed(
            type,
            readUIPreference(`${type}-channels-collapsed`),
            false,
        );
    syncChannelInfoViewport();
    loadLocalNotifications();
    window.osNotificationsSetup?.();
};
const mentionWrap = document.createElement("span");
mentionWrap.className = "mention-wrap";
mentionWrap.innerHTML =
    '<button type="button" class="fmt fmt-mention" id="mentionButton" title="メンションを追加" aria-label="メンションを追加">@</button><div class="mention-menu hidden" id="mentionMenu"><div class="mention-search"><input id="mentionSearch" type="search" placeholder="名前またはIDで検索" autocomplete="off" aria-label="メンション候補を検索"></div><div class="mention-options" id="mentionOptions"></div></div>';
const firstDivider = document.querySelector(".composer .tools .dropdown + .tool-divider");
if (firstDivider) {
    firstDivider.after(mentionWrap);
} else {
    document.querySelector(".composer .tools .dropdown")?.after(mentionWrap);
}
const aiButton = document.createElement("button");
aiButton.type = "button";
aiButton.id = "aiButton";
aiButton.className = "fmt fmt-ai hidden";
aiButton.innerHTML = icon("robot");
aiButton.title = "AIを呼び出す";
aiButton.setAttribute("aria-label", "AIを呼び出す");
aiButton.setAttribute("aria-controls", "mentionMenu");
aiButton.setAttribute("aria-expanded", "false");
mentionWrap.insertBefore(aiButton, $("mentionMenu"));
aiButton.addEventListener("mousedown", event => event.preventDefault());
S.mentionRange = null;
// Both composers use the same participant filtering; insertion stays editor-specific.
function channelMentionCandidates(channel, query = "") {
    const normalized = query.trim().toLocaleLowerCase("ja-JP");
    const candidates = new Map();
    for (const user of channel?.effectiveMembers || []) {
        if (user.id === S.me?.id || user.enabled === false) continue;
        const name = user.name || S.directory.get(user.id) || user.id;
        if (normalized && !name.toLocaleLowerCase("ja-JP").includes(normalized) &&
            !user.id.toLocaleLowerCase().includes(normalized)) continue;
        candidates.set(user.id, { id: user.id, name });
    }
    return [...candidates.values()].sort((a, b) => a.id.localeCompare(b.id));
}
function watchMentionPresence(channelID) {
    const count = S.presenceRefs.get(channelID) || 0;
    S.presenceRefs.set(channelID, count + 1);
    if (count === 0) {
        S.presence.set(channelID, null);
        if (S.ws?.readyState === WebSocket.OPEN)
            S.ws.send(JSON.stringify({type: "presence_watch", channelId: channelID}));
    }
}
function unwatchMentionPresence(channelID) {
    if (!channelID) return;
    const count = S.presenceRefs.get(channelID) || 0;
    if (count > 1) { S.presenceRefs.set(channelID, count - 1); return; }
    S.presenceRefs.delete(channelID);
    S.presence.delete(channelID);
    if (S.ws?.readyState === WebSocket.OPEN)
        S.ws.send(JSON.stringify({type: "presence_unwatch", channelId: channelID}));
}
function updateMentionPresence(option) {
    const online = S.presence.get(option.dataset.presenceChannel);
    const status = online === null || online === undefined ? "状態不明" :
        online.has(option.dataset.userId) ? "接続中" : "未接続";
    option.classList.toggle("presence-online", status === "接続中");
    option.querySelector("small").textContent = `@${option.dataset.userId} · ${status}`;
    option.setAttribute("aria-label", `${option.querySelector("b").textContent} ${status}`);
}
function handleMentionPresence(message) {
    if (!S.presenceRefs.has(message.channelId)) return;
    S.presence.set(message.channelId, message.type === "presence_snapshot"
        ? new Set(message.onlineUserIds || []) : null);
    for (const option of document.querySelectorAll(".mention-option[data-presence-channel]"))
        if (option.dataset.presenceChannel === message.channelId) updateMentionPresence(option);
}
function openMainMentionPresence() {
    const channelID = S.channel?.id;
    if (!channelID || S.mainMentionChannel === channelID) return;
    if (S.mainMentionChannel) unwatchMentionPresence(S.mainMentionChannel);
    S.mainMentionChannel = channelID;
    watchMentionPresence(channelID);
}
function closeMainMentionMenu() {
    $("mentionMenu").classList.add("hidden");
    $("aiButton").setAttribute("aria-expanded", "false");
    unwatchMentionPresence(S.mainMentionChannel);
    S.mainMentionChannel = null;
}
function mentionToken(id, name) {
    // A display name must not close the label or introduce another @recipient.
    const label = name.replace(/@/g, "＠").replace(/[（）\r\n\u2028\u2029]/g, " ").replace(/\s+/g, " ").trim();
    return `@${id}（${label || id}）`;
}
function renderMentionMenu(query = "") {
    const options = $("mentionOptions");
    options.replaceChildren();
    if (S.mentionKind === "ai") {
        for (const a of aiCandidates(query)) {
            const option = document.createElement("button");
            option.type = "button";
            option.className = "mention-option";
            const name = document.createElement("b"), id = document.createElement("small");
            name.textContent = `🤖 ${a.name}`;
            id.textContent = `@${a.id}`;
            option.append(name, id);
            option.addEventListener("mousedown", event => event.preventDefault());
            option.onclick = () => { insertMention(a.id, a.name); closeMainMentionMenu(); };
            options.append(option);
        }
        if (!options.children.length) options.textContent = "一致するAIはありません";
        return;
    }
    const normalized = query.trim().toLocaleLowerCase("ja-JP");
    const channelGroups = (S.channel?.groups || [])
        .map(groupID => ({ id: groupID, name: S.groups.get(groupID) || groupID }))
        .filter(g => !normalized || g.id.toLocaleLowerCase().includes(normalized) || g.name.toLocaleLowerCase("ja-JP").includes(normalized))
        .sort((a, b) => a.id.localeCompare(b.id));
    for (const group of channelGroups) {
        const option = document.createElement("button");
        option.type = "button";
        option.className = "mention-option";
        option.innerHTML =
            '<span class="mini">G</span><span><b></b><small></small></span>';
        option.querySelector("b").textContent = group.name;
        option.querySelector("small").textContent = "グループ · @" + group.id;
        option.onclick = () => {
            insertMention("group:" + group.id, group.name);
            closeMainMentionMenu();
        };
        options.append(option);
    }
    for (const {id, name} of channelMentionCandidates(S.channel, query)) {
        const option = document.createElement("button");
        option.type = "button";
        option.className = "mention-option";
        option.innerHTML =
            '<span class="mini"></span><span><b></b><small></small></span>';
        option.querySelector(".mini").textContent = ini(name);
        option.querySelector("b").textContent = name;
        option.querySelector("small").textContent = "@" + id;
        option.dataset.userId = id;
        option.dataset.presenceChannel = S.channel.id;
        updateMentionPresence(option);
        option.onclick = () => {
            insertMention(id, name);
            closeMainMentionMenu();
        };
        options.append(option);
    }
    if (!options.children.length)
        options.innerHTML =
            '<span class="muted mention-empty">候補がありません</span>';
}
function insertMention(id, name) {
    const range = S.mentionRange || currentEditorRange();
    if (!range) return;
    range.deleteContents();
    const mention = document.createElement("span"),
        space = document.createTextNode(" ");
    mention.className = "mention";
    mention.contentEditable = "false";
    mention.dataset.mentionToken = mentionToken(id, name);
    mention.textContent = `@${name}`;
    range.insertNode(space);
    range.insertNode(mention);
    range.setStartAfter(space);
    range.collapse(true);
    const selection = window.getSelection();
    selection.removeAllRanges();
    selection.addRange(range);
    S.mentionRange = null;
    editor.focus();
    editor.dispatchEvent(new Event("input", { bubbles: true }));
}
function openMainMention(kind) {
    if (editor.disabled) return;
    const menu = $("mentionMenu"),
        opening = menu.classList.contains("hidden") || S.mentionKind !== kind;
    closeMainMentionMenu();
    S.mentionKind = kind;
    if (opening) {
        menu.classList.remove("hidden");
        if (kind !== "ai") openMainMentionPresence();
        $("aiButton").setAttribute("aria-expanded", String(kind === "ai"));
        S.mentionRange = currentEditorRange();
        $("mentionSearch").value = "";
        renderMentionMenu();
        requestAnimationFrame(() => $("mentionSearch").focus());
    }
    else closeMainMentionMenu();
}
$("mentionButton").onclick = () => openMainMention("user");
aiButton.onclick = () => openMainMention("ai");
$("mentionSearch").oninput = (event) =>
    renderMentionMenu(event.target.value);
$("mentionSearch").addEventListener("keydown", event => {
    if (event.isComposing) return;
    if (event.key === "ArrowDown") { $("mentionOptions").querySelector("button")?.focus(); event.preventDefault(); }
    if (event.key === "Escape") { closeMainMentionMenu(); editor.focus(); }
});
$("mentionOptions").addEventListener("keydown", event => {
    const buttons = [...$("mentionOptions").querySelectorAll("button")];
    const index = buttons.indexOf(document.activeElement);
    if (["ArrowUp", "ArrowDown"].includes(event.key)) {
        event.preventDefault();
        buttons[(index + (event.key === "ArrowDown" ? 1 : buttons.length - 1)) % buttons.length]?.focus();
    }
    if (event.key === "Escape") { closeMainMentionMenu(); editor.focus(); }
});
function currentEditorRange() {
    return currentRichEditorRange(editor);
}

function textBeforeCaret() {
    const range = currentEditorRange();
    if (!range) return "";
    const before = range.cloneRange();
    before.selectNodeContents(editor);
    before.setEnd(range.endContainer, range.endOffset);
    return before.toString();
}

$("input").addEventListener("input", () => {
    const before = textBeforeCaret(),
        match = before.match(/(?:^|\s)@([^\s@]*)$/u);
    if (!match) {
        if (S.mentionRange)
            closeMainMentionMenu();
        S.mentionRange = null;
        return;
    }
    const selection = window.getSelection(), range = selection.rangeCount ? selection.getRangeAt(0).cloneRange() : null;
    if (!range) return;
    range.setStart(range.endContainer, Math.max(0, range.endOffset - match[1].length - 1));
    S.mentionRange = range;
    S.mentionKind = match[1].startsWith("ai:") ? "ai" : "user";
    const query = S.mentionKind === "ai" ? match[1].slice(3) : match[1];
    $("mentionSearch").value = query;
    renderMentionMenu(query);
    $("mentionMenu").classList.remove("hidden");
    if (S.mentionKind !== "ai") openMainMentionPresence();
});
$("input").addEventListener("keydown", (event) => {
    if (event.key === "Escape") {
        closeMainMentionMenu();
        S.mentionRange = null;
    }
});
document.addEventListener("click", (event) => {
    if (!mentionWrap.contains(event.target))
        closeMainMentionMenu();
});

const linkModal = bootstrap.Modal.getOrCreateInstance($("linkModal"));
S.linkRange = null;
S.linkElement = null;
S.linkEditor = null;

if (networkPathMode === "disabled") {
    $("linkURLLabel").textContent = "Web URL";
    $("linkURL").placeholder = "https://intranet.example";
    $("linkURLFeedback").textContent = "有効なHTTPまたはHTTPS URLを入力してください。";
}

function insertableLinkURL(value) {
    const href = safeLinkURL(value);
    if (!href) return "";
    return networkPathMode === "disabled" && new URL(href).protocol === "file:" ? "" : href;
}

function linkAtRange(range) {
    let node = range?.startContainer;
    if (node?.nodeType === Node.TEXT_NODE) node = node.parentElement;
    return node?.closest?.("a") || null;
}

function openEditorLink(root) {
    const range = currentRichEditorRange(root);
    if (!range) {
        root.focus();
        return;
    }
    S.linkEditor = root;
    S.linkRange = range;
    S.linkElement = linkAtRange(range);
    $("linkURL").value = S.linkElement?.href || "";
    $("linkURL").classList.remove("is-invalid");
    $("unlinkButton").classList.toggle("hidden", !S.linkElement);
    linkModal.show();
}

$("linkButton").onclick = () => openEditorLink(editor);

$("linkModal").addEventListener("shown.bs.modal", () => $("linkURL").focus());
$("linkModal").addEventListener("hidden.bs.modal", () => {
    const target = S.linkEditor;
    S.linkRange = null;
    S.linkElement = null;
    S.linkEditor = null;
    target?.focus();
});

function restoreLinkRange() {
    if (!S.linkRange) return false;
    const selection = window.getSelection();
    selection.removeAllRanges();
    selection.addRange(S.linkRange);
    return true;
}

$("linkForm").onsubmit = (event) => {
    event.preventDefault();
    const enteredURL = $("linkURL").value.trim(),
        href = insertableLinkURL(enteredURL);
    $("linkURL").classList.toggle("is-invalid", !href);
    if (!href || !restoreLinkRange()) return;
    const range = S.linkRange;
    if (range.collapsed) {
        const anchor = document.createElement("a");
        anchor.href = href;
        anchor.textContent = enteredURL;
        range.insertNode(anchor);
        range.setStartAfter(anchor);
        range.collapse(true);
        window.getSelection().removeAllRanges();
        window.getSelection().addRange(range);
    } else {
        document.execCommand("createLink", false, href);
    }
    for (const anchor of S.linkEditor.querySelectorAll("a")) {
        const safe = safeLinkURL(anchor.getAttribute("href") || "");
        if (!safe) anchor.replaceWith(...anchor.childNodes);
        else {
            anchor.href = safe;
            anchor.target = "_blank";
            anchor.rel = "noopener noreferrer";
        }
    }
    S.linkEditor.dispatchEvent(new Event("input", { bubbles: true }));
    linkModal.hide();
};

$("unlinkButton").onclick = () => {
    if (!restoreLinkRange()) return;
    document.execCommand("unlink", false);
    S.linkEditor.dispatchEvent(new Event("input", { bubbles: true }));
    linkModal.hide();
};

const mentionsMe = (text, ai = null) => !ai && (
    new RegExp(`(^|\\s)@${S.me?.id}(?=\\s|$|[.,!?。、！？（])`).test(text) ||
    (S.me?.groups || []).some((id) =>
        new RegExp(`(^|\\s)@group:${id}(?=\\s|$|[.,!?。、！？（])`).test(text),
    ));

function scheduleReference(ref, messageText = "", withdrawn = false) {
    if (!ref || !/^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/.test(ref.id))
        return null;
    const link = document.createElement("a");
    link.className = "schedule-message-link";
    link.href = Minihub.url("/schedules/" + encodeURIComponent(ref.id));
    link.target = "_blank";
    link.rel = "noopener noreferrer";
    link.innerHTML = icon(ref.event === "finalized" ? "calendar-check" : "calendar3");
    const finalized = ref.event === "finalized",
        prefix = finalized ? "予定を確定しました: " : "予定調整を公開しました: ";
    // Announcement text also supplies titles for previously saved messages.
    let title = messageText.startsWith(prefix) ? messageText.slice(prefix.length) : "";
    if (finalized)
        title = title.replace(/（\d{4}-\d{2}-\d{2}(?: \d{2}:\d{2}(?:〜\d{2}:\d{2})?)）$/, "");
    const copy = document.createElement("span");
    copy.className = "schedule-message-copy";
    if (title) {
        const heading = document.createElement("span");
        heading.className = "schedule-message-title";
        heading.textContent = title;
        copy.append(heading);
    }
    const action = document.createElement("span");
    action.className = "schedule-message-action";
    action.textContent = withdrawn ? "取り消した予定調整を開く" : finalized
        ? "確定した予定を開く"
        : "予定調整を開く";
    copy.append(action);
    link.append(copy);
    return link;
}

loadChannels = async function () {
    if (!S.groups.size) await loadGroups();
    if (!S.directory.size) {
        const users = await api("/api/users/directory");
        S.directory = new Map(
            users.map((user) => [user.id, user.name]),
        );
        await reloadAIAccounts();
    }
    const [channels, readResult] = await Promise.all([
        api("/api/channels"),
        api("/api/channels/read-statuses").catch(() => null),
    ]);
    S.channels = channels;
    const selected = S.channels.find((channel) => channel.id === S.channel?.id);
    if (selected) {
        S.channel.members = selected.members;
        S.channel.groups = selected.groups;
        S.channel.type = selected.type;
        renderChannelAction();
    }
    for (const channel of S.channels) {
        const status = readResult?.statuses?.[channel.id] ||
            S.read.get(channel.id) || {
                lastReadSeq: 0,
                latestSeq: 0,
                unreadCount: 0,
            };
        S.read.set(channel.id, status);
        S.unread.set(channel.id, member(channel) ? status.unreadCount : 0);
    }
    for (const type of ["public", "private"]) {
        const host = $(type);
        host.replaceChildren();
        const channels = S.channels
            .filter((channel) => channel.type === type)
            .sort((left, right) => {
                const membershipOrder = Number(member(right)) - Number(member(left));
                return membershipOrder || left.id.localeCompare(right.id);
            });
        updateChannelSectionSummary(type, channels);
        for (const c of channels) {
            const button = document.createElement("button");
            const direct = directMember(c),
                inherited = groupMember(c),
                membershipLabel = inherited
                    ? "グループ経由で参加"
                    : direct
                      ? "参加中"
                      : "未参加",
                membershipIcon = inherited
                    ? "people-fill"
                    : direct
                      ? "check-circle-fill"
                      : "dash-circle";
            button.className =
                "channel" +
                (S.channel?.id === c.id ? " active" : "") +
                (type === "public"
                    ? inherited
                        ? " joined via-group"
                        : direct
                          ? " joined"
                          : " unjoined"
                    : " joined");
            const count = member(c) ? S.unread.get(c.id) || 0 : 0;
            button.classList.toggle("has-unread", count > 0);
            if (S.channel?.id === c.id) button.setAttribute("aria-current", "true");
            button.innerHTML = `<span class="channel-kind">${type === "private" ? icon("lock-fill") : "#"}</span><span class="channel-name"></span>${type === "public" ? `<span class="channel-membership ${inherited ? "via-group" : direct ? "joined" : "unjoined"}" aria-hidden="true">${icon(membershipIcon)}</span>` : ""}${count ? `<i class="tc-badge" aria-hidden="true">${count > 99 ? "99+" : count}</i>` : ""}`;
            button.querySelector(".channel-name").textContent = c.name;
            button.title = type === "public"
                ? `${c.name}（${membershipLabel}）`
                : `${c.name}（非公開）`;
            button.setAttribute(
                "aria-label",
                `${c.name}、${type === "public" ? membershipLabel : "非公開チャンネル"}${count ? `、未読${count}件` : ""}`,
            );
            if (member(c) && S.read.get(c.id)?.threadUpdates) {
                const updates = document.createElement("span");
                updates.className = "channel-thread-update";
                updates.textContent = "返信";
                button.append(updates);
                button.setAttribute("aria-label", button.getAttribute("aria-label") + "、返信更新あり");
            }
            button.dataset.channelId = c.id;
            button.onclick = () => {
                closeMobileChannelNav("title");
                select(c.id);
            };
            host.append(button);
        }
        if (!channels.length) {
            const empty = document.createElement("div");
            empty.className = "channel-list-empty";
            empty.textContent = "チャンネルはありません";
            host.append(empty);
        }
    }
    filterChannelList();
};

function beginInitialLoading(channelID, selectionToken) {
    cancelLoadingTimer("initial");
    cancelLoadingTimer("older");
    S.initialLoading = true;
    S.initialLoadingVisible = false;
    S.initialError = "";
    S.msgs = [];
    S.nextBefore = null;
    S.loadingOlder = false;
    S.olderLoadingVisible = false;
    S.historyError = "";
    S.catchupError = "";
    $("input").disabled = $("send").disabled = true;
    $("input").placeholder = "履歴を読み込んでいます…";
    updateHistoryBusy();
    render();
    showLoadingAfterDelay(
        "initial",
        () =>
            S.initialLoading &&
            S.channel?.id === channelID &&
            S.selectionToken === selectionToken,
        () => {
            S.initialLoadingVisible = true;
        },
    );
}

function finishInitialLoading(channelID, selectionToken, error = "") {
    if (S.channel?.id !== channelID || S.selectionToken !== selectionToken)
        return;
    cancelLoadingTimer("initial");
    S.initialLoading = false;
    S.initialLoadingVisible = false;
    S.initialError = error;
    updateHistoryBusy();
    const canPost = !error && member(S.channel);
    $("input").disabled = $("send").disabled = !canPost;
    $("input").placeholder = error
        ? "履歴を再読み込みしてください"
        : canPost
          ? `${S.channel.name} にメッセージを送信`
          : "投稿するにはチャンネルへ参加してください";
    render();
    if (!error) positionInitialMessages();
    if (!error && S.pending) {
        S.pending = false;
        queueMicrotask(catchup);
    }
}

select = async function (id) {
    if (S.channel?.id !== id) resetMemberSearch();
	stopTyping();
	S.typers.clear();
	renderTypingStatus();
    const selectionToken = ++S.selectionToken;
    clearMessageHighlight();
    S.channelActionBusy = true;
    renderChannelAction();
    let selectedChannel;
    try {
        selectedChannel = await api(
            "/api/channels/" + encodeURIComponent(id),
        );
    } catch (error) {
        if (selectionToken === S.selectionToken) {
            S.channelActionBusy = false;
            renderChannelAction();
            note(error.message, true);
        }
        return;
    }
    if (selectionToken !== S.selectionToken) return;
    if (typeof threadChannelChanging === "function") threadChannelChanging(id);
    S.channel = selectedChannel;
	if (typeof window.pollChannelSelected === "function") window.pollChannelSelected();
    if (typeof updateThreadControls === "function") updateThreadControls();
	$("manageCurrentChannel").classList.toggle("hidden", !selectedChannel.capabilities?.manageMembers);
    S.channelActionBusy = false;
    renderChannelAction();
    beginInitialLoading(id, selectionToken);
    try {
        const status = await api(
            `/api/channels/${encodeURIComponent(id)}/read`,
        );
    if (selectionToken !== S.selectionToken) return;
    S.read.set(id, status);
    S.unread.set(id, status.unreadCount);
    await loadChannels();
    if (selectionToken !== S.selectionToken) return;
    const c = S.channel;
    $("title").innerHTML = (c.type === "private" ? icon("lock-fill") : "#") + " <span></span>";
    $("title").querySelector("span").textContent = c.name;
    $("desc").textContent = c.id;
    $("info").textContent =
        (c.type === "public" ? "公開" : "非公開") +
        " / 作成者 " +
        c.createdBy;
    renderChannelMembers();
    renderChannelAction(c);
    const page = await api(
        `/api/channels/${encodeURIComponent(id)}/messages?limit=100`,
    );
    if (selectionToken !== S.selectionToken || S.channel?.id !== id) return;
    S.msgs = page.messages;
    S.nextBefore = page.nextBefore;
    S.loadingOlder = false;
    S.historyError = "";
    S.reactions.clear();
    if (S.msgs.length) await syncReactions(id, S.msgs[0].seq - 1);
    if (selectionToken !== S.selectionToken || S.channel?.id !== id) return;
    finishInitialLoading(id, selectionToken);
    } catch (error) {
        finishInitialLoading(id, selectionToken, error.message);
    }
};

function clearMessageHighlight() {
    clearTimeout(S.messageHighlightTimer);
    S.messageHighlightTimer = null;
    S.highlightedMessage = null;
    document
        .querySelectorAll(".message.notification-target")
        .forEach((message) => message.classList.remove("notification-target"));
}

function highlightMessage(target, seq, channelID) {
    clearMessageHighlight();
    S.highlightedMessage = { channelID, seq };
    target.classList.add("notification-target");
    S.messageHighlightTimer = setTimeout(clearMessageHighlight, 3000);
}

function messageIsHighlighted(channelID, seq) {
    return (
        S.highlightedMessage?.channelID === channelID &&
        S.highlightedMessage?.seq === seq
    );
}

function focusMessage(seq, channelID, highlight = false) {
    if (S.channel?.id !== channelID) return;
    const host = $("messages"),
        selector = `.message[data-seq="${seq}"]`;
    const focus = () => {
        if (S.channel?.id !== channelID) return "cancel";
        const target = host.querySelector(selector);
        if (!target) return false;
        target.focus({ preventScroll: true });
        target.scrollIntoView({ behavior: "smooth", block: "end" });
        if (highlight) highlightMessage(target, seq, channelID);
        return true;
    };
    if (focus()) return;
    const observer = new MutationObserver(() => {
        if (focus()) observer.disconnect();
    });
    observer.observe(host, { childList: true });
    setTimeout(() => observer.disconnect(), 5000);
}

function createUnreadLine(label, count) {
    const line = document.createElement("div"),
        copy = document.createElement("span"),
        button = document.createElement("button");
    line.className = "unread-line";
    copy.className = "unread-copy";
    copy.textContent = label;
    button.type = "button";
    button.className = "btn btn-outline-secondary btn-sm mark-read-button";
    button.innerHTML = `${icon("check2")}<span>すべて既読にする</span>`;
    button.setAttribute("aria-label", `未読${count}件をすべて既読にする`);
    button.disabled = S.markingRead;
    button.onclick = () => markAllRead(button);
    line.append(copy, button);
    return line;
}

function positionInitialMessages() {
    const host = $("messages");
    if (!S.msgs.length) return;
    // A sticky unread line can appear at the top even while its real position is above the viewport.
    host.scrollTop = 0;
    const unread = member(S.channel) && (S.read.get(S.channel.id)?.unreadCount || 0) > 0;
    const line = unread ? host.querySelector(".unread-line") : null;
    if (line) {
        host.scrollTop += line.getBoundingClientRect().top - host.getBoundingClientRect().top - host.clientTop;
    } else {
        host.scrollTop = host.scrollHeight;
    }
}

function messageViewportAnchor(host) {
    const bottom = host.scrollHeight - host.scrollTop - host.clientHeight;
    const top = host.getBoundingClientRect().top;
    const firstVisible = [...host.querySelectorAll(".message")].find(
        message => message.getBoundingClientRect().bottom > top,
    );
    return {
        atBottom: bottom <= 24,
        scrollTop: host.scrollTop,
        seq: firstVisible?.dataset.seq,
        top: firstVisible?.getBoundingClientRect().top,
    };
}

function restoreMessageViewport(host, anchor) {
    if (anchor.atBottom) {
        host.scrollTop = host.scrollHeight;
        return;
    }
    const message = anchor.seq && host.querySelector(`.message[data-seq="${anchor.seq}"]`);
    host.scrollTop = message
        ? anchor.scrollTop + message.getBoundingClientRect().top - anchor.top
        : anchor.scrollTop;
}

async function markAllRead(button) {
    const channelID = S.channel?.id,
        seq = S.msgs.at(-1)?.seq;
    if (!channelID || !seq || S.markingRead) return;
    S.markingRead = true;
    button.disabled = true;
    let committed = false;
    try {
        await api(`/api/channels/${encodeURIComponent(channelID)}/read`, {
            method: "PUT",
            body: JSON.stringify({ seq }),
        });
        committed = true;
        const status = await api(`/api/channels/${encodeURIComponent(channelID)}/read`);
        S.read.set(channelID, status);
        S.unread.set(channelID, status.unreadCount);
        await loadChannels();
    } catch (error) {
        note(error.message, true);
    } finally {
        S.markingRead = false;
        if (S.channel?.id !== channelID) return;
        if (committed) render();
        else
            document
                .querySelector(".mark-read-button")
                ?.removeAttribute("disabled");
    }
}

function chatEmptyState(channel) {
    const state = document.createElement("div"),
        heading = document.createElement("h2"),
        detail = document.createElement("p");
    state.className = "chat-empty";
    state.innerHTML = icon("chat-square-text");
    heading.textContent = channel
        ? "まだメッセージはありません"
        : "チャンネルを選択してください";
    detail.textContent = !channel
        ? "チャンネル一覧から、会話するチャンネルを選べます。"
        : member(channel)
          ? "下の入力欄から、最初のメッセージを送信できます。"
          : "画面上部の参加ボタンから、このチャンネルに参加できます。";
    state.append(heading, detail);
    return state;
}

render = function () {
    const host = $("messages");
    const previousChannel = host.dataset.renderChannel;
    const anchor = !S.initialLoading && previousChannel === S.channel?.id &&
        host.querySelector(".message") ? messageViewportAnchor(host) : null;
    host.dataset.renderChannel = S.channel?.id || "";
    host.replaceChildren();
    renderChannelAction();
    const lastRead = S.read.get(S.channel?.id)?.lastReadSeq || 0;
    const joined = member(S.channel);
    let boundary = false,
        day = "";
    if (S.initialLoading) {
        if (S.initialLoadingVisible)
            host.append(
                historyStatus(
                    "履歴を読み込んでいます…",
                    "history-status-initial",
                ),
            );
        return;
    }
    if (S.initialError) {
        host.append(
            historyStatus(
                "履歴を取得できませんでした",
                "history-status-initial history-status-error",
                () => select(S.channel.id),
            ),
        );
        return;
    }
    if (!S.msgs.length) {
        if (S.catchupLoadingVisible)
            host.append(
                historyStatus(
                    "新着を確認しています…",
                    "history-status-initial",
                ),
            );
        else if (S.catchupError)
            host.append(
                historyStatus(
                    "新着を取得できませんでした",
                    "history-status-initial history-status-error",
                    catchup,
                ),
            );
        else host.append(chatEmptyState(S.channel));
        return;
    }
    if (S.olderLoadingVisible)
        host.append(historyStatus("過去の履歴を読み込んでいます…"));
    else if (S.historyError)
        host.append(
            historyStatus(
                "過去の履歴を取得できませんでした",
                "history-status-error",
                () => loadOlder(true),
            ),
        );
    const readStatus = S.read.get(S.channel?.id) || {};
    if (joined && readStatus.unreadCount > S.msgs.filter(m => m.seq > lastRead && m.userId !== S.me.id).length) {
        host.append(
            createUnreadLine(
                `未読${readStatus.unreadCount}件のうち最新${S.msgs.length}件を表示`,
                readStatus.unreadCount,
            ),
        );
        boundary = true;
    }
    let prevMsg = null, newDay = false, boundaryJustAdded = false;
    const dayKey = (d) => `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;
    const dayLabel = (d) => {
        const now = new Date(), yesterday = new Date(now);
        yesterday.setDate(now.getDate() - 1);
        const weekday = d.toLocaleDateString("ja-JP", { weekday: "short" });
        const base = d.toLocaleDateString("ja-JP", d.getFullYear() === now.getFullYear()
            ? { month: "long", day: "numeric" }
            : { year: "numeric", month: "long", day: "numeric" });
        const rel = dayKey(d) === dayKey(now) ? "今日 · " : dayKey(d) === dayKey(yesterday) ? "昨日 · " : "";
        return `${rel}${base}（${weekday}）`;
    };
    for (const m of S.msgs) {
        const date = new Date(m.ts),
            label = dayKey(date);
        if (label !== day) {
            const sep = document.createElement("div");
            sep.className = "date-sep";
            sep.textContent = dayLabel(date);
            host.append(sep);
            day = label;
            newDay = true;
        }
        if (joined && !boundary && readStatus.unreadCount > 0 && m.seq > lastRead && m.userId !== S.me.id) {
            const count = readStatus.unreadCount;
            host.append(
                createUnreadLine(`ここから未読 ${count}件`, count),
            );
            boundary = true;
            boundaryJustAdded = true;
        }
        const article = document.createElement("article");
        const continued =
            !!prevMsg &&
            !newDay &&
            !boundaryJustAdded &&
            !prevMsg.withdrawnAt &&
            !m.withdrawnAt &&
            !prevMsg.pollRef && !prevMsg.scheduleRef && !m.pollRef && !m.scheduleRef &&
            prevMsg.userId === m.userId &&
            (prevMsg.ai?.id || "") === (m.ai?.id || "") &&
            date - new Date(prevMsg.ts) < 5 * 60 * 1000;
        prevMsg = m;
        newDay = false;
        boundaryJustAdded = false;
        article.className =
            "message" + (continued ? " continued" : "") +
			(m.withdrawnAt ? " withdrawn" : "") +
            (m.userId === S.me.id ? " mine" : "") +
            (mentionsMe(m.text, m.ai) ? " mentioned" : "") +
            (messageIsHighlighted(S.channel?.id, m.seq)
                ? " notification-target"
                : "");
        article.dataset.seq = String(m.seq);
        article.tabIndex = -1;
        article.innerHTML =
            '<div><div class="head"><span></span><i class="time"></i><i class="seq"></i></div><div class="text"></div></div>';
        const author = messageAuthor(m);
        article.querySelector(".head span").textContent = author;
        if (m.userId === S.me.id) {
            const mine = document.createElement("em");
            mine.className = "mine-label";
            mine.textContent = "自分";
            article.querySelector(".head span").after(mine);
        }
        article.querySelector(".time").textContent =
            date.toLocaleTimeString("ja-JP", {
                hour: "2-digit",
                minute: "2-digit",
            });
        article.querySelector(".seq").textContent = `seq ${m.seq}`;
		article.querySelector(".text").append(renderMessageText(m));
		if (!m.withdrawnAt && m.attachments && m.attachments.length) {
			const atts = renderAttachments(m.attachments);
			if (atts) article.querySelector(".text").append(atts);
		}
		const scheduleLink = scheduleReference(m.scheduleRef, m.text, !!m.withdrawnAt);
		if (scheduleLink) article.querySelector(".text").append(document.createElement("br"), scheduleLink);
		if (m.pollRef && typeof window.pollRenderCard === "function") {
			const pollHost = document.createElement("div");
			pollHost.className = "poll-card-host";
			article.querySelector(".text").append(pollHost);
			window.pollRenderCard(pollHost,m.pollRef.id);
		}
		const actions = m.withdrawnAt ? document.createElement("div") : reactionBar(m.seq, m.userId !== S.me.id);
		if (m.withdrawnAt) actions.className = "message-actions";
		if (!m.withdrawnAt) {
			if (typeof replyButton === "function") actions.append(replyButton(m));
		}
		if (m.withdrawnAt && typeof replyButton === "function" && T.summaries.get(`${S.channel.id}:${m.seq}`)?.replyCount) actions.append(replyButton(m));
		const canManageMessage = m.userId === S.me.id || S.me.role === "admin" || (S.channel?.managers || []).includes(S.me.id) && member(S.channel);
		const canWithdraw = !m.withdrawnAt && !m.pollRef && !m.scheduleRef && canManageMessage;
		if (canWithdraw) {
			const button = document.createElement("button");
			button.type = "button"; button.className = "btn btn-sm message-withdraw-action"; button.textContent = "取り消す";
			button.onclick = async () => {
				if (!confirm("この投稿を取り消しますか？ 元の内容は保存されます。")) return;
				button.disabled = true;
				try { const updated = await api(`/api/channels/${encodeURIComponent(S.channel.id)}/messages/${m.seq}/withdraw`, {method:"POST"}); S.msgs = S.msgs.map(x => x.seq === m.seq ? updated : x); render(); }
				catch (error) { button.disabled = false; note(error.message, true); }
			};
			actions.append(button);
		}
		if (m.withdrawnAt && m.withdrawnKind === "message" && canManageMessage) {
			const button=document.createElement("button");button.type="button";button.className="btn btn-sm message-withdraw-action message-restore-action";button.textContent="取り消しを戻す";
			button.onclick=async()=>{if(!confirm("取り消しを戻し、元のメッセージを再表示しますか？"))return;button.disabled=true;try{const updated=await api(`/api/channels/${encodeURIComponent(S.channel.id)}/messages/${m.seq}/restore`,{method:"POST"});S.msgs=S.msgs.map(x=>x.seq===m.seq?updated:x);render()}catch(error){button.disabled=false;note(error.message,true)}};
			actions.append(button);
		}
        article.children[0].append(actions);
        host.append(article);
    }
    if (S.catchupLoadingVisible)
        host.append(
            historyStatus(
                "新着を確認しています…",
                "history-status-catchup",
            ),
        );
    else if (S.catchupError)
        host.append(
            historyStatus(
                "新着を取得できませんでした",
                "history-status-catchup history-status-error",
                catchup,
            ),
        );
    if (anchor && previousChannel === S.channel?.id) restoreMessageViewport(host, anchor);
};

catchup = async function () {
    if (!S.channel) return;
    if (S.initialLoading) {
        S.pending = true;
        return;
    }
    if (S.catching) {
        S.pending = true;
        return;
    }
    S.catching = true;
    S.catchupLoadingVisible = false;
    S.catchupError = "";
    const loadingChannelID = S.channel.id,
        loadingSelectionToken = S.selectionToken;
    updateHistoryBusy();
    showLoadingAfterDelay(
        "catchup",
        () =>
            S.catching &&
            S.channel?.id === loadingChannelID &&
            S.selectionToken === loadingSelectionToken,
        () => {
            S.catchupLoadingVisible = true;
        },
    );
    try {
        const channelID = S.channel.id;
        let nextAfter = S.msgs.at(-1)?.seq || 0,
            incoming = [];
        do {
            const page = await api(
                `/api/channels/${encodeURIComponent(channelID)}/messages?after=${nextAfter}&limit=500`,
            );
            incoming.push(...page.messages);
            nextAfter = page.nextAfter;
        } while (nextAfter !== null && S.channel?.id === channelID);
        if (S.channel?.id !== channelID) return;
        const known = new Set(S.msgs.map((x) => x.seq));
        for (const message of incoming)
            if (!known.has(message.seq)) {
                S.msgs.push(message);
                known.add(message.seq);
            }
        if (
            incoming.some(
                (message) =>
                    message.userId !== S.me.id &&
                    mentionsMe(message.text, message.ai),
            )
        )
            note("あなた宛てのメンションがあります");
        S.msgs.sort((a, b) => a.seq - b.seq);
        if (incoming.length) {
            const status = await api(
                `/api/channels/${encodeURIComponent(channelID)}/read`,
            );
            if (S.channel?.id !== channelID) return;
            S.read.set(channelID, status);
            S.unread.set(channelID, status.unreadCount);
            render();
            loadChannels();
        }
    } catch (error) {
        if (
            S.channel?.id === loadingChannelID &&
            S.selectionToken === loadingSelectionToken
        ) {
            S.catchupLoadingVisible = false;
            S.catchupError = error.message;
        }
    } finally {
        cancelLoadingTimer("catchup");
        S.catching = false;
        S.catchupLoadingVisible = false;
        updateHistoryBusy();
        if (
            S.channel?.id === loadingChannelID &&
            S.selectionToken === loadingSelectionToken
        )
            render();
        if (S.pending) {
            S.pending = false;
            queueMicrotask(catchup);
        }
    }
};

async function loadOlder(force = false) {
    if (
        !S.channel ||
        S.loadingOlder ||
        S.nextBefore === null ||
        (S.historyError && !force)
    )
        return;
    const channelID = S.channel.id,
        selectionToken = S.selectionToken,
        before = S.nextBefore,
        host = $("messages"),
        oldHeight = host.scrollHeight,
        oldTop = host.scrollTop;
    S.loadingOlder = true;
    S.olderLoadingVisible = false;
    S.historyError = "";
    updateHistoryBusy();
    showLoadingAfterDelay(
        "older",
        () =>
            S.loadingOlder &&
            S.channel?.id === channelID &&
            S.selectionToken === selectionToken,
        () => {
            S.olderLoadingVisible = true;
        },
    );
    try {
        const page = await api(
            `/api/channels/${encodeURIComponent(channelID)}/messages?before=${before}&limit=100`,
        );
        if (S.channel?.id !== channelID || selectionToken !== S.selectionToken)
            return;
        const known = new Set(S.msgs.map((message) => message.seq));
        const older = page.messages.filter((message) => !known.has(message.seq));
        S.msgs.unshift(...older);
        S.nextBefore = page.nextBefore;
        if (older.length)
            await syncReactions(channelID, older[0].seq - 1, false);
        if (S.channel?.id !== channelID || selectionToken !== S.selectionToken)
            return;
        cancelLoadingTimer("older");
        S.loadingOlder = false;
        S.olderLoadingVisible = false;
        updateHistoryBusy();
        render();
        host.scrollTop = oldTop + host.scrollHeight - oldHeight;
    } catch (error) {
        if (S.channel?.id !== channelID || selectionToken !== S.selectionToken)
            return;
        cancelLoadingTimer("older");
        S.loadingOlder = false;
        S.olderLoadingVisible = false;
        S.historyError = error.message;
        updateHistoryBusy();
        render();
    }
}

$("messages").addEventListener("scroll", () => {
    if ($("messages").scrollTop < 80) loadOlder();
});

realtime = function () {
    const socket = new WebSocket(
        Minihub.websocketURL(),
    );
    S.ws = socket;
    socket.onopen = () => {
        if (S.ws === socket) {
            socket.send(JSON.stringify({type: "online_count_watch"}));
            for (const channelID of S.presenceRefs.keys())
                socket.send(JSON.stringify({type: "presence_watch", channelId: channelID}));
            setConnectionStatus("接続中", true);
            catchup();
			document.dispatchEvent(new Event("poll-reconnect"));
            if (typeof syncThreads === "function") syncThreads();
			updateOwnTyping();
            if (S.channel) {
                const channelID = S.channel.id;
                syncReactions(channelID, Math.max(0, (S.msgs[0]?.seq || 1) - 1))
                    .then(async () => {
                        if (typeof T === "undefined" || typeof syncThreadReactions !== "function") return;
                        const thread = T.current;
                        if (thread?.channel !== channelID) return;
                        await syncThreadReactions(thread);
                        if (T.current === thread) for (const item of thread.messages) refreshThreadReaction(item.seq);
                    }).catch(() => {});
            }
        }
    };
    socket.onmessage = async (event) => {
        if (S.ws !== socket) return;
        let message;
        try {
            message = JSON.parse(event.data);
        } catch {
            return;
        }
        if (message.type === "online_count") {
            const badge = $("onlineUsersBadge");
            const countSpan = $("onlineUsersCount");
            if (badge && countSpan && typeof message.onlineCount === "number") {
                const count = message.onlineCount;
                countSpan.textContent = count === 1 ? "1 user" : `${count} users`;
                badge.classList.remove("hidden");
            }
            return;
        }
        if (message.type === "typing_start" || message.type === "typing_stop") {
			handleTypingEvent(message);
			return;
        }
		if (message.type === "presence_snapshot" || message.type === "presence_unavailable") {
			handleMentionPresence(message);
			return;
		}
		if (message.type === "channels_changed") {
            closeMainMentionMenu();
            if (typeof closeThreadMention === "function") closeThreadMention();
			const selectedID = S.channel?.id;
			await loadChannels();
			if (selectedID && S.channels.some((channel) => channel.id === selectedID))
				await select(selectedID);
			else if (selectedID) clearSelectedChannel();
            syncThreads();
			return;
		}
		if (message.type === "reaction_changed") {
            invalidateReactionUsers(message.channelId, message.messageSeq);
            if (S.channel?.id === message.channelId) {
                const views = await api(
                    `/api/channels/${encodeURIComponent(message.channelId)}/reactions?messageSeqs=${message.messageSeq}`,
                ).catch(() => []);
                const view = views.find((item) => item.messageSeq === message.messageSeq);
                applyReactionView(view || { messageSeq: message.messageSeq, reactions: [] });
                render();
                if (typeof refreshThreadReaction === "function") refreshThreadReaction(message.messageSeq);
            }
            return;
        }
		if (message.type === "poll_changed") {
			document.dispatchEvent(new CustomEvent("poll-changed",{detail:message}));
			return;
		}
		if (message.type === "message_changed") {
			loadMentions().catch(()=>{});
			if (S.channel?.id === message.channelId) {
				const page = await api(`/api/channels/${encodeURIComponent(message.channelId)}/messages?around=${message.seq}&limit=1`).catch(()=>null);
				const changed = page?.messages?.find(m=>m.seq===message.seq);
				if (changed) { S.msgs = S.msgs.map(m=>m.seq===changed.seq?changed:m); render(); }
				if (T.current?.channel===message.channelId) {
					const thread = T.current;
					const query = thread.root===message.seq ? "" : `&around=${message.seq}`;
					const page = await api(`${threadURL(thread)}/messages?limit=1${query}`).catch(()=>null);
					if (page && T.current===thread) {
						thread.parent=page.root;
						const changedReply=page.messages?.find(m=>m.seq===message.seq);
						if (changedReply) thread.messages=thread.messages.map(m=>m.seq===message.seq?changedReply:m);
						renderThread(thread);
					}
				}
			}
			return;
		}
        if (message.type === "thread_updated") {
            handleThreadEvent(message);
            return;
        }
        if (message.type !== "new_message") return;
        const selected = S.channel?.id === message.channelId;
        if (selected) catchup();
        else await loadChannels();
        if (selected && (typeof osNotificationEnabled !== "function" || !osNotificationEnabled())) return;
        const channel = S.channels.find(
            (c) => c.id === message.channelId,
        );
        if (!channel || !member(channel)) return;
        try {
            const page = await api(
                `/api/channels/${encodeURIComponent(message.channelId)}/messages?after=${message.seq - 1}&limit=1`,
            );
            const posted = page.messages.find((item) => item.seq === message.seq);
            if (!posted) return;
            const mentioned = posted.mentionUserIds?.includes(S.me.id) || mentionsMe(posted.text, posted.ai);
            if (!selected && mentioned)
                note(
                    `${channel?.name || message.channelId} であなた宛てのメンションがあります`,
                );
            else if (!selected)
                note(
                    `${channel?.name || message.channelId} に新しいメッセージがあります`,
                );
            if (posted.userId !== S.me.id && typeof showOSNotification === "function")
                void showOSNotification({
                    channelId: message.channelId, channelName: channel.name,
                    seq: posted.seq, mention: mentioned,
                    body: mentioned ? "あなた宛てのメンションがあります" : "新しい投稿があります",
                });
        } catch (_) {}
    };
    socket.onclose = () => {
        if (S.ws === socket) {
            S.ws = null;
			for (const channelID of S.presenceRefs.keys())
				handleMentionPresence({type: "presence_unavailable", channelId: channelID});
			stopTyping(false);
			S.typers.clear();
			renderTypingStatus();
            setConnectionStatus("再接続待ち");
            setTimeout(realtime, 2000);
        }
    };
    socket.onerror = () => socket.close();
};

