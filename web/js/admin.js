// Admin management: channels, groups, AI accounts, and users.

// --- Channel Creation ---
$("addChannel").onclick = () =>
    bootstrap.Modal.getOrCreateInstance($("channelModal")).show();
$("channelForm").onsubmit = async (e) => {
    e.preventDefault();
    const submit = $("createChannelSubmit");
    if (submit.disabled) return;
    submit.disabled = true;
    submit.textContent = "作成中…";
    try {
        let c = await api("/api/channels", {
            method: "POST",
            body: JSON.stringify({
                id: $("cid").value.trim(),
                name: $("cname").value.trim(),
                type: $("ctype").value,
            }),
        });
        bootstrap.Modal.getOrCreateInstance($("channelModal")).hide();
        e.target.reset();
        note("チャンネルを作成しました。参加設定は「チャンネル管理」から変更できます。");
        try {
            await loadChannels();
            await select(c.id);
        } catch (error) {
            note(`作成は完了しましたが、画面の読み込みに失敗しました: ${error.message}`, true);
        }
    } catch (x) {
        note(x.message, true);
    } finally {
        submit.disabled = false;
        submit.textContent = "チャンネルを作成";
    }
};

// --- Groups, Channels & AI Accounts Management ---
S.groupItems = [];
S.selectedGroupID = "";
S.groupDetail = null;

const groupMemberCount = (groupID) =>
    (S.managedUsers || []).filter((user) =>
        (user.groups || []).includes(groupID),
    ).length;

function renderGroupList() {
    const query = $("groupSearch").value.trim().toLocaleLowerCase("ja-JP");
    const groups = S.groupItems.filter((group) =>
        !query || group.id.toLocaleLowerCase().includes(query) ||
        group.name.toLocaleLowerCase("ja-JP").includes(query));
    const host = $("groupList");
    host.replaceChildren();
    $("groupCount").textContent = `${groups.length} / ${S.groupItems.length} 件`;
    for (const group of groups) {
        const row = document.createElement("button");
        row.type = "button";
        row.className = "group-row";
        row.classList.toggle("selected", group.id === S.selectedGroupID);
        row.innerHTML = '<span><b></b><small></small></span><span class="group-row-count"></span>';
        row.querySelector("b").textContent = group.name;
        row.querySelector("small").textContent = group.id;
        row.querySelector(".group-row-count").textContent = `${groupMemberCount(group.id)} 名`;
        row.onclick = () => selectManagedGroup(group.id);
        host.append(row);
    }
    if (!groups.length) {
        const empty = document.createElement("div");
        empty.className = "group-list-empty";
        empty.textContent = query ? "該当するグループはありません" : "グループが登録されていません";
        host.append(empty);
    }
}

function showGroupPane(pane) {
    $("groupEmpty").classList.toggle("hidden", pane !== "empty");
    $("groupForm").classList.toggle("hidden", pane !== "create");
    $("groupDetail").classList.toggle("hidden", pane !== "detail");
}

async function renderGroups(selectID = S.selectedGroupID) {
    const [groups] = await Promise.all([loadGroups(), listUsers()]);
    S.groupItems = groups;
    S.selectedGroupID = groups.some((group) => group.id === selectID) ? selectID : "";
    renderGroupList();
    if (S.selectedGroupID) await selectManagedGroup(S.selectedGroupID);
    else showGroupPane(groups.length ? "empty" : "create");
}

async function selectManagedGroup(groupID) {
    try {
        S.selectedGroupID = groupID;
        S.groupDetail = await api(`/api/groups/${encodeURIComponent(groupID)}`);
        $("groupAddPanel").classList.add("hidden");
        $("groupMemberSearch").value = "";
        $("groupCandidateSearch").value = "";
        renderGroupList();
        renderGroupDetail();
        showGroupPane("detail");
    } catch (error) { note(error.message, true); }
}

const userStatus = (user) => !user.enabled ? "無効" : user.role === "admin" ? "管理者" : "一般";

function renderUserChecks(hostID, users, query, kind) {
    const host = $(hostID), normalized = query.trim().toLocaleLowerCase("ja-JP");
    const filtered = users.filter((user) => !normalized ||
        user.id.toLocaleLowerCase().includes(normalized) ||
        user.name.toLocaleLowerCase("ja-JP").includes(normalized));
    host.replaceChildren();
    for (const user of filtered) {
        const label = document.createElement("label");
        label.className = "group-check-row";
        label.innerHTML = '<input class="form-check-input" type="checkbox"><span></span><small></small>';
        const input = label.querySelector("input");
        input.value = user.id;
        input.dataset.kind = kind;
        input.onchange = () => {
            if (kind === "member") $("removeGroupMembers").disabled = !selectedUserIDs("member").length;
        };
        label.querySelector("span").textContent = `${user.name} (@${user.id})`;
        label.querySelector("small").textContent = userStatus(user);
        host.append(label);
    }
    if (!filtered.length) {
        const empty = document.createElement("div");
        empty.className = "group-list-empty";
        empty.textContent = normalized ? "該当するユーザーはいません" : "対象ユーザーはいません";
        host.append(empty);
    }
}

const selectedUserIDs = (kind) => Array.from(
    document.querySelectorAll(`input[data-kind="${kind}"]:checked`),
    (input) => input.value,
);

// Channel maintenance is shared by workspace administrators and delegated channel managers.
S.managedChannels = [];
S.managedChannelDetail = null;

function resetMemberSearch() {
    $("memberSearch").value = "";
}

function renderChannelMembers() {
    const members = S.channel?.effectiveMembers || [];
    const query = $("memberSearch").value.trim().toLocaleLowerCase("ja-JP");
    const displayName = (user) => user.name || S.directory.get(user.id) || user.id;
    const visible = members.filter((user) => !query ||
        displayName(user).toLocaleLowerCase("ja-JP").includes(query) ||
        user.id.toLocaleLowerCase("ja-JP").includes(query));
    $("memberCount").textContent = visible.length + " / " + members.length + " 名";
    $("memberSearch").disabled = !S.channel;
    const host = $("members");
    host.replaceChildren();
    for (const user of visible) {
        const name = displayName(user), row = document.createElement("div");
        row.className = "member";
        row.innerHTML = '<span class="mini" aria-hidden="true"></span><span class="member-name"></span><span class="role"></span>';
        row.children[0].textContent = ini(name);
        row.children[1].textContent = name;
        row.children[1].title = name;
        const routes = user.direct ? ["個別参加"] : [];
        for (const groupID of user.viaGroups || []) routes.push(S.groups.get(groupID) || groupID);
        row.children[2].textContent = routes.join(" / ");
        row.children[2].title = routes.join(" / ");
        if (user.id === S.me?.id) {
            const self = document.createElement("span");
            self.className = "member-self";
            self.textContent = "自分";
            row.append(self);
        }
        host.append(row);
    }
    if (!visible.length) {
        const empty = document.createElement("p");
        empty.className = "muted";
        empty.textContent = !S.channel ? "チャンネルを選択してください" :
            members.length ? "該当する参加者はいません" : "参加者はいません";
        host.append(empty);
    }
}
$("memberSearch").addEventListener("input", renderChannelMembers);

function clearSelectedChannel() {
    closeMainMentionMenu();
    if (typeof closeThread === "function") closeThread(false);
    stopTyping();
    S.channel = null;
    if (typeof updateThreadControls === "function") updateThreadControls();
    S.msgs = [];
    $("title").textContent = "チャンネルを選択";
    $("desc").textContent = "チャンネル一覧から選択してください";
    $("info").textContent = "未選択";
    resetMemberSearch();
    renderChannelMembers();
    $("messages").innerHTML = '<div class="muted">メッセージはここに表示されます</div>';
    $("input").disabled = $("send").disabled = true;
    $("join").classList.add("hidden");
    $("manageCurrentChannel").classList.add("hidden");
}

async function loadManagedChannels(preferredID = "") {
    const includeArchived = S.me.role === "admin" && $("showArchivedChannels").checked;
    S.managedChannels = await api(`/api/channels?management=true${includeArchived ? "&includeArchived=true" : ""}`);
    const list = $("managedChannelList");
    list.replaceChildren();
    for (const channel of S.managedChannels) {
        const row = document.createElement("button");
        row.type = "button";
        row.className = "managed-channel-row";
        row.dataset.channelId = channel.id;
        row.innerHTML = "<b></b><small></small>";
        row.querySelector("b").textContent = channel.name;
        row.querySelector("small").textContent = `${channel.type === "private" ? "非公開" : "公開"} · ${channel.id}${channel.archivedAt ? " · アーカイブ済み" : ""}`;
        row.onclick = () => {
            if (confirmMembershipDiscard()) loadChannelManagement(channel.id).catch((error) => note(error.message, true));
        };
        list.append(row);
    }
    if (!S.managedChannels.length) {
        const empty = document.createElement("div");
        empty.className = "group-empty";
        empty.textContent = "管理できるチャンネルはありません。";
        list.append(empty);
    }
    const target = preferredID && S.managedChannels.some((item) => item.id === preferredID)
        ? preferredID : S.managedChannels[0]?.id;
    if (target) await loadChannelManagement(target);
    else {
        S.managedChannelDetail = null;
        membershipEditor = null;
        $("channelManagementEmpty").classList.remove("hidden");
        $("channelManagementDetail").classList.add("hidden");
    }
}

const membershipLabels = { members: "個別参加メンバー", groups: "参加グループ", managers: "チャンネル管理者" };
let membershipEditor = null;
let membershipBusy = false;
let managementRequest = 0;

function resetMembershipEditor() {
    const detail = S.managedChannelDetail;
    membershipEditor = {
        draft: Object.fromEntries(Object.keys(membershipLabels).map((kind) => [kind, new Set(detail[kind] || [])])),
        tab: "members", adding: false, candidates: new Set(), query: "", sort: "name",
    };
    renderMembershipEditor();
}

function membershipDeltas() {
    return Object.fromEntries(Object.keys(membershipLabels).map((kind) =>
        [kind, idDelta(S.managedChannelDetail[kind], membershipEditor.draft[kind])]));
}

function membershipDirty() {
    return !!membershipEditor && !!S.managedChannelDetail &&
        (Object.values(membershipDeltas()).some((delta) => delta.add.length || delta.remove.length) || membershipEditor.candidates.size > 0);
}

function confirmMembershipDiscard() {
    return !membershipBusy && (!membershipDirty() || window.confirm("未保存の参加設定を破棄して続けますか？"));
}

function renderMembershipEditor() {
    const editor = membershipEditor, detail = S.managedChannelDetail;
    if (!editor || !detail) return;
    const { tab, draft, adding } = editor;
    const readOnly = !!detail.archivedAt || !detail.capabilities.manageMembers;
    document.querySelectorAll("[data-membership-tab]").forEach((button) => {
        const kind = button.dataset.membershipTab, active = kind === tab;
        button.textContent = `${membershipLabels[kind]} (${draft[kind].size})`;
        button.setAttribute("aria-selected", String(active));
        button.tabIndex = active ? 0 : -1;
        button.disabled = membershipBusy;
        if (active) $("channelMembershipPanel").setAttribute("aria-labelledby", button.id);
    });
    $("channelMembershipHint").textContent = tab === "members"
        ? "グループ経由ではなく、ユーザー単位で参加しているメンバーです。公開チャンネルに自分で参加したユーザーも含みます。個別参加を解除しても、参加グループに所属していれば参加が続きます。" + (detail.type === "public" ? " 公開チャンネルでは解除後も再参加できます。" : "")
        : tab === "groups" ? "このグループに所属する有効なユーザーが参加できます。"
        : "管理者は個別参加メンバーにもなります。後任を追加してから自分を外せます。管理者の解除後も個別参加は維持されます。";
    $("channelMembershipSearch").value = editor.query;
    $("channelMembershipSort").value = editor.sort;
    $("addChannelMembers").classList.toggle("hidden", adding || readOnly);
    $("addChannelMembers").disabled = membershipBusy;
    $("channelCandidateActions").classList.toggle("hidden", !adding);
    const entries = tab === "groups" ? Array.from(S.groups, ([id, name]) => ({ id, name })) : (detail.users || []);
    const known = new Map(entries.map((entry) => [entry.id, entry]));
    for (const id of draft[tab]) if (!known.has(id)) known.set(id, { id, name: id, enabled: false });
    const eligible = [...known.values()].filter((entry) => adding ? !draft[tab].has(entry.id) && entry.enabled !== false : draft[tab].has(entry.id));
    const query = editor.query.trim().toLocaleLowerCase("ja-JP");
    const visible = eligible.filter((entry) => `${entry.name} ${entry.id}`.toLocaleLowerCase("ja-JP").includes(query));
    visible.sort((a, b) => (editor.sort === "name" ? a.name.localeCompare(b.name, "ja") : 0) || a.id.localeCompare(b.id, "ja"));
    $("channelMembershipCount").textContent = `${adding ? "追加候補" : "設定済み"}: ${visible.length} / ${eligible.length} 件`;
    const host = $("channelMembershipList");
    host.setAttribute("aria-label", adding ? "追加候補一覧" : "設定済み一覧");
    host.replaceChildren();
    for (const entry of visible) {
        const row = document.createElement(adding ? "label" : "div");
        row.className = "channel-membership-row";
        const identity = document.createElement("span");
        identity.className = "channel-membership-identity";
        const name = document.createElement("span"), id = document.createElement("small");
        name.textContent = entry.name;
        id.textContent = entry.id + (entry.enabled === false ? " · 無効" : "") + (tab === "members" && draft.managers.has(entry.id) ? " · 管理者" : "");
        identity.append(name, id);
        if (adding) {
            const check = document.createElement("input");
            check.type = "checkbox";
            check.className = "form-check-input";
            check.checked = editor.candidates.has(entry.id);
            check.onchange = () => {
                if (check.checked) editor.candidates.add(entry.id); else editor.candidates.delete(entry.id);
                updateMembershipActions();
            };
            row.append(check, identity);
        } else {
            const remove = document.createElement("button");
            remove.type = "button";
            remove.className = "btn btn-outline-secondary btn-sm";
            remove.textContent = tab === "members" ? "個別参加を解除" : tab === "managers" ? "管理者を解除" : "解除";
            remove.setAttribute("aria-label", `${entry.name} (${entry.id}) の${remove.textContent}`);
            const locked = tab === "members" && draft.managers.has(entry.id) || tab === "managers" && draft.managers.size === 1;
            remove.disabled = readOnly || membershipBusy || locked;
            if (locked) remove.title = tab === "members" ? "先に管理者から外してください" : "管理者は1名以上必要です";
            remove.onclick = () => {
                const index = visible.indexOf(entry), scroll = host.scrollTop;
                draft[tab].delete(entry.id);
                renderMembershipEditor();
                const buttons = host.querySelectorAll("button:not(:disabled)");
                (buttons[Math.min(index, buttons.length - 1)] || host).focus({ preventScroll: true });
                host.scrollTop = scroll;
            };
            row.append(identity, remove);
        }
        host.append(row);
    }
    if (!visible.length) {
        const empty = document.createElement("p");
        empty.className = "group-empty";
        empty.textContent = query ? "検索条件に一致する項目はありません。" : adding ? "追加できる候補はありません。" : "まだ設定されていません。「追加」から設定できます。";
        host.append(empty);
    }
    updateMembershipActions();
}

function updateMembershipActions() {
    const deltas = Object.values(membershipDeltas());
    const add = deltas.reduce((sum, delta) => sum + delta.add.length, 0);
    const remove = deltas.reduce((sum, delta) => sum + delta.remove.length, 0);
    const readOnly = !!S.managedChannelDetail.archivedAt || !S.managedChannelDetail.capabilities.manageMembers;
    $("channelMembershipChanges").textContent = membershipBusy ? "保存中…" : add || remove ? `未保存: 追加 ${add} 件・解除 ${remove} 件（設定項目数）` : "未保存の変更はありません";
    $("saveChannelMembership").disabled = membershipBusy || !(add || remove) || membershipEditor.adding;
    $("saveChannelMembership").classList.toggle("hidden", readOnly);
    $("resetChannelMembership").disabled = membershipBusy || !membershipDirty();
    $("resetChannelMembership").classList.toggle("hidden", readOnly);
    $("applyChannelCandidates").textContent = `選択した${membershipEditor.candidates.size}件を追加`;
    $("applyChannelCandidates").disabled = !membershipEditor.candidates.size;
}

async function loadChannelManagement(channelID) {
    const request = ++managementRequest;
    const detail = await api(`/api/channels/${encodeURIComponent(channelID)}/management`);
    if (request !== managementRequest) return;
    S.managedChannelDetail = detail;
    document.querySelectorAll(".managed-channel-row").forEach((row) => row.classList.toggle("selected", row.dataset.channelId === channelID));
    $("channelManagementEmpty").classList.add("hidden");
    $("channelManagementDetail").classList.remove("hidden");
    $("managedChannelTitle").textContent = detail.name;
    $("managedChannelID").textContent = detail.id;
    $("managedChannelName").value = detail.name;
    $("managedChannelType").value = detail.type;
    $("managedChannelArchived").classList.toggle("hidden", !detail.archivedAt);
    const canSettings = detail.capabilities.manageSettings && !detail.archivedAt;
    $("managedChannelName").disabled = !canSettings;
    $("managedChannelType").disabled = !canSettings;
    $("saveChannelSettings").classList.toggle("hidden", !canSettings);
    resetMembershipEditor();
    $("archiveChannel").classList.toggle("hidden", !detail.capabilities.archive);
    $("restoreChannel").classList.toggle("hidden", !detail.capabilities.restore);
}

const idDelta = (before, after) => {
    const oldSet = new Set(before || []), newSet = new Set(after || []);
    return { add: [...newSet].filter((id) => !oldSet.has(id)), remove: [...oldSet].filter((id) => !newSet.has(id)) };
};

async function openChannelManagement(channelID = "") {
    $("showArchivedChannels").checked = false;
    $("showArchivedChannels").closest("label").classList.toggle("hidden", S.me.role !== "admin");
    await loadManagedChannels(channelID);
    bootstrap.Modal.getOrCreateInstance($("channelManagementModal")).show();
}

$("manageChannels").onclick = () => openChannelManagement();
$("manageCurrentChannel").onclick = () => S.channel && openChannelManagement(S.channel.id);
$("showArchivedChannels").onchange = () => {
    if (!confirmMembershipDiscard()) { $("showArchivedChannels").checked = !$("showArchivedChannels").checked; return; }
    loadManagedChannels(S.managedChannelDetail?.id || "").catch((error) => note(error.message, true));
};
$("channelManagementModal").addEventListener("hide.bs.modal", (event) => {
    if (!confirmMembershipDiscard()) event.preventDefault();
});
document.querySelectorAll("[data-membership-tab]").forEach((button, index, buttons) => {
    button.onclick = () => {
        if (membershipEditor.adding && membershipEditor.candidates.size && !window.confirm("追加候補の選択を破棄してタブを切り替えますか？")) return;
        Object.assign(membershipEditor, { tab: button.dataset.membershipTab, adding: false, candidates: new Set(), query: "" });
        renderMembershipEditor();
    };
    button.onkeydown = (event) => {
        const next = event.key === "ArrowRight" ? (index + 1) % buttons.length : event.key === "ArrowLeft" ? (index + buttons.length - 1) % buttons.length : event.key === "Home" ? 0 : event.key === "End" ? buttons.length - 1 : -1;
        if (next < 0) return;
        event.preventDefault();
        buttons[next].click();
        document.querySelector('[data-membership-tab][aria-selected="true"]').focus();
    };
});
$("channelMembershipSearch").oninput = (event) => { membershipEditor.query = event.target.value; renderMembershipEditor(); };
$("channelMembershipSort").onchange = (event) => { membershipEditor.sort = event.target.value; renderMembershipEditor(); };
$("addChannelMembers").onclick = () => {
    Object.assign(membershipEditor, { adding: true, candidates: new Set(), query: "" });
    renderMembershipEditor();
    $("channelMembershipSearch").focus();
};
$("cancelChannelCandidates").onclick = () => {
    Object.assign(membershipEditor, { adding: false, candidates: new Set(), query: "" });
    renderMembershipEditor();
    $("addChannelMembers").focus();
};
$("applyChannelCandidates").onclick = () => {
    const { draft, tab, candidates } = membershipEditor;
    for (const id of candidates) {
        draft[tab].add(id);
        if (tab === "managers") draft.members.add(id);
    }
    $("cancelChannelCandidates").click();
};
$("resetChannelMembership").onclick = () => {
    resetMembershipEditor();
    $("channelMembershipSearch").focus();
};

$("channelSettingsForm").onsubmit = async (event) => {
    event.preventDefault();
    const detail = S.managedChannelDetail, nextType = $("managedChannelType").value;
    if (!confirmMembershipDiscard()) return;
    if (detail.type === "public" && nextType === "private" && !window.confirm("現在の個別参加メンバーと参加グループを維持したまま非公開に変更します。続けますか？")) return;
    try {
        await api(`/api/channels/${encodeURIComponent(detail.id)}`, { method: "PATCH", body: JSON.stringify({ name: $("managedChannelName").value.trim(), type: nextType }) });
        await loadChannels();
        await loadManagedChannels(detail.id);
        if (S.channel?.id === detail.id) await select(detail.id);
        note("チャンネル設定を保存しました");
    } catch (error) { note(error.message, true); }
};

$("saveChannelMembership").onclick = async () => {
    if (membershipBusy || membershipEditor.adding) return;
    const detail = S.managedChannelDetail;
    const { managers, members, groups } = membershipDeltas();
    membershipBusy = true;
    renderMembershipEditor();
    try {
        await api(`/api/channels/${encodeURIComponent(detail.id)}/members`, { method: "PATCH", body: JSON.stringify({ addUsers: members.add, removeUsers: members.remove, addGroups: groups.add, removeGroups: groups.remove, addManagers: managers.add, removeManagers: managers.remove }) });
    } catch (error) {
        membershipBusy = false;
        renderMembershipEditor();
        note(error.message, true);
        return;
    }
    // The write succeeded. A later refresh failure must not offer the same edits for resubmission.
    for (const kind of Object.keys(membershipLabels)) detail[kind] = [...membershipEditor.draft[kind]];
    try {
        await loadChannels();
        await loadManagedChannels(detail.id);
        if (S.channel?.id === detail.id) {
            try { await select(detail.id); } catch { clearSelectedChannel(); }
        }
        note("チャンネルの参加設定を保存しました");
    } catch (error) { note(`保存は完了しましたが、画面の再読み込みに失敗しました: ${error.message}`, true); }
    finally { membershipBusy = false; renderMembershipEditor(); }
};

$("archiveChannel").onclick = async () => {
    const detail = S.managedChannelDetail;
    if (!confirmMembershipDiscard()) return;
    if (!window.confirm(`「${detail.name}」をアーカイブします。\n\n利用者の一覧から消え、投稿できなくなります。履歴と予定調整データは保持されます。`)) return;
    try {
        await api(`/api/channels/${encodeURIComponent(detail.id)}/archive`, { method: "POST" });
        if (S.channel?.id === detail.id) clearSelectedChannel();
        await loadChannels();
        await loadManagedChannels();
        note("チャンネルをアーカイブしました");
    } catch (error) { note(error.message, true); }
};

$("restoreChannel").onclick = async () => {
    const detail = S.managedChannelDetail;
    try {
        await api(`/api/channels/${encodeURIComponent(detail.id)}/restore`, { method: "POST" });
        await loadChannels();
        await loadManagedChannels(detail.id);
        note("チャンネルを復元しました");
    } catch (error) { note(error.message, true); }
};

function renderGroupMembers() {
    const members = S.groupDetail?.members || [];
    $("groupMemberCount").textContent = `${members.length} 名`;
    renderUserChecks("groupMembers", members, $("groupMemberSearch").value, "member");
    $("removeGroupMembers").disabled = true;
}

function renderGroupCandidates() {
    const memberIDs = new Set((S.groupDetail?.members || []).map((user) => user.id));
    renderUserChecks("groupCandidates", (S.managedUsers || []).filter((user) =>
        !memberIDs.has(user.id)), $("groupCandidateSearch").value, "candidate");
}

function renderGroupDetail() {
    const detail = S.groupDetail;
    $("groupDetailTitle").textContent = detail.name;
    $("groupDetailID").textContent = `ID: ${detail.id}`;
    $("groupName").value = detail.name;
    renderGroupMembers();
    renderGroupCandidates();
    const channels = $("groupChannels");
    channels.replaceChildren();
    for (const channel of detail.channels || []) {
        const row = document.createElement("div");
        row.className = "group-channel-row";
        row.innerHTML = "<span></span><small></small>";
        row.querySelector("span").textContent = channel.name;
        row.querySelector("small").textContent = `${channel.type === "private" ? "非公開" : "公開"} · ${channel.id}`;
        channels.append(row);
    }
    if (!channels.children.length) {
        const empty = document.createElement("div");
        empty.className = "group-list-empty";
        empty.textContent = "割り当てられたチャンネルはありません";
        channels.append(empty);
    }
}

async function refreshAfterGroupChange() {
    S.managedUsers = await api("/api/users");
    S.me = { ...S.me, ...(await api("/api/me")) };
    await loadChannels();
    if (S.channel && !S.channels.some((channel) => channel.id === S.channel.id)) {
        S.channel = null;
        S.msgs = [];
        $("title").textContent = "チャンネルを選択";
        $("desc").textContent = "チャンネル一覧から選択してください";
        $("messages").innerHTML = '<div class="muted">メッセージはここに表示されます</div>';
        $("input").disabled = $("send").disabled = true;
    }
}

async function changeManagedGroupMembers(add, remove) {
    S.groupDetail = await api(`/api/groups/${encodeURIComponent(S.selectedGroupID)}/members`, {
        method: "PATCH", body: JSON.stringify({ add, remove }),
    });
    await refreshAfterGroupChange();
    renderGroupDetail();
    renderGroupList();
}

$("groups").onclick = async () => {
    $("groupSearch").value = "";
    $("groupMemberSearch").value = "";
    $("groupCandidateSearch").value = "";
    await renderGroups();
    bootstrap.Modal.getOrCreateInstance($("groupModal")).show();
};
$("groupSearch").oninput = renderGroupList;
$("groupMemberSearch").oninput = renderGroupMembers;
$("groupCandidateSearch").oninput = renderGroupCandidates;
$("newGroup").onclick = () => {
    S.selectedGroupID = "";
    S.groupDetail = null;
    $("groupForm").reset();
    renderGroupList();
    showGroupPane("create");
};
$("toggleAddMembers").onclick = () => {
    $("groupAddPanel").classList.toggle("hidden");
    renderGroupCandidates();
};
$("addGroupMembers").onclick = async () => {
    const add = selectedUserIDs("candidate");
    if (!add.length) return note("追加するユーザーを選択してください", true);
    try {
        await changeManagedGroupMembers(add, []);
        $("groupAddPanel").classList.add("hidden");
        note(`${add.length} 名をグループに追加しました`);
    } catch (error) { note(error.message, true); }
};
$("removeGroupMembers").onclick = async () => {
    const remove = selectedUserIDs("member");
    if (!remove.length) return;
    try {
        await changeManagedGroupMembers([], remove);
        note(`${remove.length} 名をグループから解除しました`);
    } catch (error) { note(error.message, true); }
};
$("groupNameForm").onsubmit = async (event) => {
    event.preventDefault();
    try {
        const group = await api(`/api/groups/${encodeURIComponent(S.selectedGroupID)}`, {
            method: "PUT", body: JSON.stringify({ name: $("groupName").value.trim() }),
        });
        S.groupDetail.name = group.name;
        await renderGroups(group.id);
        note("グループ名を変更しました");
    } catch (error) { note(error.message, true); }
};
$("deleteGroup").onclick = async () => {
    const detail = S.groupDetail;
    const message = `「${detail.name}」を削除します。\n\n所属ユーザー: ${detail.members.length} 名\n割当チャンネル: ${detail.channels.length} 件\n\n所属とチャンネル割当も解除されます。この操作は元に戻せません。`;
    if (!window.confirm(message)) return;
    try {
        await api(`/api/groups/${encodeURIComponent(detail.id)}`, { method: "DELETE" });
        S.selectedGroupID = "";
        S.groupDetail = null;
        await refreshAfterGroupChange();
        await renderGroups();
        note("グループを削除しました");
    } catch (error) { note(error.message, true); }
};
$("groupForm").onsubmit = async (event) => {
    event.preventDefault();
    try {
        const id = $("gid").value.trim();
        await api("/api/groups", {
            method: "POST",
            body: JSON.stringify({
                id: $("gid").value.trim(),
                name: $("gname").value.trim(),
            }),
        });
        event.target.reset();
        await renderGroups(id);
        note("グループを登録しました");
    } catch (error) {
        note(error.message, true);
    }
};

function renderAIGroupCheckboxes(selectedGroupIDs = []) {
    const host = $("aiGroupsList");
    host.replaceChildren();
    const selectedSet = new Set(selectedGroupIDs || []);
    const groups = Array.from(S.groups.entries()).map(([id, name]) => ({ id, name }));
    for (const group of groups) {
        const label = document.createElement("label");
        label.className = "group-check-row";
        label.innerHTML = '<input class="form-check-input" type="checkbox"><span></span><small></small>';
        const input = label.querySelector("input");
        input.value = group.id;
        input.checked = selectedSet.has(group.id);
        label.querySelector("span").textContent = group.name;
        label.querySelector("small").textContent = group.id;
        host.append(label);
    }
    if (!groups.length) {
        const empty = document.createElement("div");
        empty.className = "group-list-empty";
        empty.textContent = "登録されているグループはありません";
        host.append(empty);
    }
}

function getSelectedAIGroupIDs() {
    return Array.from(
        document.querySelectorAll("#aiGroupsList input[type='checkbox']:checked"),
        (input) => input.value,
    );
}

function showAIPane(mode, account = null) {
    $("aiEmpty").classList.toggle("hidden", mode !== "empty");
    $("aiForm").classList.toggle("hidden", mode === "empty");
    if (mode === "create") {
        $("aiDetailTitle").textContent = "新規AI登録";
        $("aiDetailID").textContent = "";
        $("deleteAI").classList.add("hidden");
        $("aiID").disabled = false;
        $("aiID").value = "";
        $("aiName").value = "";
        $("aiEnabled").checked = true;
        $("aiURL").value = "";
        $("aiModel").value = "";
        $("aiTimeout").value = "";
        $("aiToken").value = "";
        $("aiToken").placeholder = "トークンを入力";
        $("aiTokenStatus").textContent = "";
        $("aiTokenEnv").value = "";
        $("aiSystemPrompt").value = "";
        $("aiTemperature").value = "";
        $("aiTopP").value = "";
        $("aiMaxTokens").value = "";
        renderAIGroupCheckboxes([]);
    } else if (mode === "edit" && account) {
        $("aiDetailTitle").textContent = account.name || "AI設定";
        $("aiDetailID").textContent = `ID: ${account.id}`;
        $("deleteAI").classList.remove("hidden");
        $("aiID").disabled = true;
        $("aiID").value = account.id;
        $("aiName").value = account.name || "";
        $("aiEnabled").checked = account.enabled !== false;
        $("aiURL").value = account.url || "";
        $("aiModel").value = account.model || "";
        $("aiTimeout").value = account.timeout || "";
        $("aiToken").value = "";
        $("aiToken").placeholder = account.tokenConfigured ? "設定済み（変更する場合のみ入力）" : "未設定（変更する場合に入力）";
        $("aiTokenStatus").textContent = account.tokenConfigured ? "※APIトークンが設定されています。変更しない場合は空欄のままにしてください。" : "";
        $("aiTokenEnv").value = account.tokenEnv || "";
        $("aiSystemPrompt").value = account.systemPrompt || "";
        $("aiTemperature").value = account.temperature != null ? account.temperature : "";
        $("aiTopP").value = account.topP != null ? account.topP : "";
        $("aiMaxTokens").value = account.maxTokens != null ? account.maxTokens : "";
        renderAIGroupCheckboxes(account.groups || []);
    }
}

function renderAIList() {
    const query = $("aiSearch").value.trim().toLocaleLowerCase("ja-JP");
    const accounts = (S.managedAIAccounts || []).filter((a) =>
        !query || a.id.toLocaleLowerCase().includes(query) ||
        (a.name || "").toLocaleLowerCase("ja-JP").includes(query),
    );
    const host = $("aiList");
    host.replaceChildren();
    $("aiCount").textContent = `${accounts.length} / ${(S.managedAIAccounts || []).length} 件`;
    for (const a of accounts) {
        const row = document.createElement("button");
        row.type = "button";
        row.className = "group-row";
        row.classList.toggle("selected", a.id === S.selectedAIID);
        row.innerHTML = '<span><b></b><small></small></span><span class="group-row-count"></span>';
        row.querySelector("b").textContent = a.name;
        row.querySelector("small").textContent = a.id;
        row.querySelector(".group-row-count").textContent = !a.enabled ? "無効" : a.groups?.length ? `${a.groups.length} グループ` : "全ユーザー";
        row.onclick = () => selectManagedAI(a.id);
        host.append(row);
    }
    if (!accounts.length) {
        const empty = document.createElement("div");
        empty.className = "group-list-empty";
        empty.textContent = query ? "該当するAIはありません" : "AIが登録されていません";
        host.append(empty);
    }
}

async function selectManagedAI(id) {
    S.selectedAIID = id;
    renderAIList();
    try {
        const account = await api(`/api/admin/ai-accounts/${encodeURIComponent(id)}`);
        showAIPane("edit", account);
    } catch (error) {
        note(`AI設定を取得できません: ${error.message}`, true);
    }
}

async function loadManagedAIAccounts(selectID = S.selectedAIID) {
    await loadGroups();
    try {
        S.managedAIAccounts = await api("/api/admin/ai-accounts");
    } catch (error) {
        note(`AI一覧を取得できません: ${error.message}`, true);
        S.managedAIAccounts = [];
    }
    S.selectedAIID = S.managedAIAccounts.some((a) => a.id === selectID) ? selectID : "";
    renderAIList();
    if (S.selectedAIID) {
        await selectManagedAI(S.selectedAIID);
    } else {
        showAIPane(S.managedAIAccounts.length ? "empty" : "create");
    }
}

async function reloadAIAccounts() {
    S.aiAccounts = await api("/api/ai-accounts").catch((error) => {
        note(`AI一覧を取得できません: ${error.message}`, true);
        return [];
    });
    $("aiButton")?.classList.toggle("hidden", !S.aiAccounts.length);
    $("threadAIButton")?.classList.toggle("hidden", !S.aiAccounts.length);
    window.updateAICanvasAccounts?.();
    return S.aiAccounts;
}

$("aiSettings").onclick = async () => {
    $("aiSearch").value = "";
    await loadManagedAIAccounts();
    bootstrap.Modal.getOrCreateInstance($("aiModal")).show();
};
$("aiSearch").oninput = renderAIList;
$("newAI").onclick = () => {
    S.selectedAIID = "";
    renderAIList();
    showAIPane("create");
    $("aiID").focus();
};
$("deleteAI").onclick = async () => {
    if (!S.selectedAIID) return;
    const account = S.managedAIAccounts?.find((a) => a.id === S.selectedAIID);
    const name = account ? account.name : S.selectedAIID;
    if (!window.confirm(`「${name}」を削除しますか？\n\nこの操作は元に戻せません。`)) return;
    try {
        await api(`/api/admin/ai-accounts/${encodeURIComponent(S.selectedAIID)}`, { method: "DELETE" });
        S.selectedAIID = "";
        note("AIを削除しました");
        await reloadAIAccounts();
        await loadManagedAIAccounts();
    } catch (error) {
        note(error.message, true);
    }
};
$("aiForm").onsubmit = async (event) => {
    event.preventDefault();
    const token = $("aiToken").value;
    const tokenEnv = $("aiTokenEnv").value.trim();
    if (token && tokenEnv) {
        return note("APIトークンと環境変数名はどちらか一方のみ指定してください", true);
    }
    const tempVal = $("aiTemperature").value.trim();
    const topPVal = $("aiTopP").value.trim();
    const maxTokensVal = $("aiMaxTokens").value.trim();
    const temperature = tempVal === "" ? null : Number(tempVal);
    const topP = topPVal === "" ? null : Number(topPVal);
    const maxTokens = maxTokensVal === "" ? null : parseInt(maxTokensVal, 10);

    const payload = {
        name: $("aiName").value.trim(),
        enabled: $("aiEnabled").checked,
        url: $("aiURL").value.trim(),
        model: $("aiModel").value.trim(),
        timeout: $("aiTimeout").value.trim(),
        token,
        tokenEnv,
        groups: getSelectedAIGroupIDs(),
        systemPrompt: $("aiSystemPrompt").value,
        temperature,
        topP,
        maxTokens,
    };

    if (S.selectedAIID) {
        try {
            await api(`/api/admin/ai-accounts/${encodeURIComponent(S.selectedAIID)}`, {
                method: "PUT",
                body: JSON.stringify(payload),
            });
            note("AI設定を保存しました");
            await reloadAIAccounts();
            await loadManagedAIAccounts(S.selectedAIID);
        } catch (error) {
            note(error.message, true);
        }
    } else {
        const id = $("aiID").value.trim();
        payload.id = id;
        try {
            await api("/api/admin/ai-accounts", {
                method: "POST",
                body: JSON.stringify(payload),
            });
            note("AIを登録しました");
            await reloadAIAccounts();
            await loadManagedAIAccounts(id);
        } catch (error) {
            note(error.message, true);
        }
    }
};

// --- User Management ---
const split = (x) =>
    x
        .split(",")
        .map((y) => y.trim())
        .filter(Boolean);

function renderReadOnlyGroups(groupIDs) {
    const host = $("editgroups");
    host.replaceChildren();
    for (const id of groupIDs || []) {
        const chip = document.createElement("span");
        chip.className = "readonly-group";
        chip.textContent = `${S.groups.get(id) || id} (${id})`;
        host.append(chip);
    }
    if (!host.children.length) {
        const empty = document.createElement("span");
        empty.className = "muted";
        empty.textContent = "所属なし";
        host.append(empty);
    }
}

function editUser(u, row) {
    document
        .querySelectorAll("#userList .user")
        .forEach((x) => x.classList.toggle("selected", x === row));
    $("editid").value = u.id;
    $("editname").value = u.name;
    $("editpw").value = "";
    $("editrole").value = u.role;
    renderReadOnlyGroups(u.groups);
    $("editenabled").checked = u.enabled;
    $("one").classList.add("hidden");
    $("bulk").classList.add("hidden");
    $("editUser").classList.remove("hidden");
}

async function createUsers(users, form) {
    try {
        let r = await api("/api/users", {
            method: "POST",
            body: JSON.stringify({ users }),
        });
        form.reset();
        await listUsers();
        S.directory.clear();
        note(r.length + "名を登録しました");
    } catch (x) {
        note(x.message, true);
    }
}
$("one").onsubmit = (e) => {
    e.preventDefault();
    createUsers(
        [
            {
                id: $("newid").value.trim(),
                name: $("newname").value.trim(),
                password: $("newpw").value,
                role: $("newrole").value,
                groups: selectedGroups("newgroups"),
            },
        ],
        e.target,
    );
};
$("bulk").onsubmit = (e) => {
    e.preventDefault();
    try {
        let users = $("bulktext")
            .value.split(/\r?\n/)
            .map((x) => x.trim())
            .filter((x) => x && !x.startsWith("#"))
            .map((x, i) => {
                let p = x.split("\t");
                if (p.length < 3)
                    throw Error(`${i + 1}行目は3列以上必要です`);
                return {
                    id: p[0],
                    name: p[1],
                    password: p[2],
                    role: p[3] || "user",
                    groups: split(p[4] || ""),
                };
            });
        createUsers(users, e.target);
    } catch (x) {
        note(x.message, true);
    }
};
$("editUser").onsubmit = async (e) => {
    e.preventDefault();
    try {
        let id = $("editid").value,
            updated = await api(
                "/api/users/" + encodeURIComponent(id),
                {
                    method: "PUT",
                    body: JSON.stringify({
                        name: $("editname").value.trim(),
                        password: $("editpw").value,
                        role: $("editrole").value,
                        enabled: $("editenabled").checked,
                    }),
                },
            );
        await listUsers();
        S.directory.clear();
        if (updated.id === S.me.id) {
            S.me.name = updated.name;
            S.me.role = updated.role;
            S.me.groups = updated.groups || [];
            $("me").textContent = updated.name;
            $("avatar").textContent = ini(updated.name);
            $("admin").classList.toggle(
                "hidden",
                updated.role !== "admin",
            );
        }
        note("ユーザー設定を変更しました");
        $("editUser").classList.add("hidden");
        $("one").classList.remove("hidden");
    } catch (x) {
        note(x.message, true);
    }
};

// Scalable user management: searchable list and an independently scrolling editor.
S.managedUsers = [];
function renderManagedUsers() {
    const query = $("userSearch")
        .value.trim()
        .toLocaleLowerCase("ja-JP");
    const users = S.managedUsers.filter(
        (user) =>
            !query ||
            user.id.toLocaleLowerCase().includes(query) ||
            user.name.toLocaleLowerCase("ja-JP").includes(query),
    );
    const host = $("userList");
    host.replaceChildren();
    $("userCount").textContent =
        `${users.length} / ${S.managedUsers.length} 名`;
    for (const user of users) {
        const row = document.createElement("button");
        row.type = "button";
        row.className = "user";
        row.dataset.userId = user.id;
        row.innerHTML =
            '<span class="user-avatar"></span><span class="user-copy"><b></b><small></small></span><span class="user-role"></span>';
        row.querySelector(".user-avatar").textContent = ini(
            user.name,
        );
        row.querySelector("b").textContent = user.name;
        row.querySelector("small").textContent = "@" + user.id;
        const role = row.querySelector(".user-role");
        role.textContent = user.enabled
            ? user.role === "admin"
                ? "管理者"
                : "一般"
            : "無効";
        role.classList.toggle(
            "admin",
            user.enabled && user.role === "admin",
        );
        role.classList.toggle("disabled", !user.enabled);
        row.onclick = () => {
            document
                .querySelectorAll(".tabs .btn")
                .forEach((button) =>
                    button.classList.remove("active"),
                );
            editUser(user, row);
        };
        host.append(row);
    }
    if (!users.length) {
        const empty = document.createElement("div");
        empty.className = "user-empty";
        empty.textContent = query
            ? "該当するユーザーはいません"
            : "ユーザーが登録されていません";
        host.append(empty);
    }
}
listUsers = async function () {
    S.managedUsers = await api("/api/users");
    renderManagedUsers();
    return S.managedUsers;
};
$("userSearch").oninput = renderManagedUsers;
$("users").onclick = async () => {
    $("userSearch").value = "";
    await listUsers();
    $("editUser").classList.add("hidden");
    $("bulk").classList.add("hidden");
    $("one").classList.remove("hidden");
    document
        .querySelectorAll(".tabs .btn")
        .forEach((button) =>
            button.classList.toggle(
                "active",
                button.dataset.tab === "one",
            ),
        );
    bootstrap.Modal.getOrCreateInstance($("userModal")).show();
};
document.querySelectorAll("[data-tab]").forEach(
    (button) =>
        (button.onclick = () => {
            $("one").classList.toggle(
                "hidden",
                button.dataset.tab !== "one",
            );
            $("bulk").classList.toggle(
                "hidden",
                button.dataset.tab !== "bulk",
            );
            $("editUser").classList.add("hidden");
            document
                .querySelectorAll("#userList .user")
                .forEach((row) => row.classList.remove("selected"));
            document
                .querySelectorAll(".tabs .btn")
                .forEach((tab) =>
                    tab.classList.toggle("active", tab === button),
                );
        }),
);

// Export symbols for app integration
window.reloadAIAccounts = reloadAIAccounts;
window.listUsers = listUsers;
window.renderGroups = renderGroups;
window.openChannelManagement = openChannelManagement;
