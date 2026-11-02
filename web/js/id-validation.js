// Client-side feedback for IDs. Mirrors the server's ID format, but names the
// offending character because full-width letters are hard to spot by eye.
(() => {
    const HINT = "IDには半角英数字と「_」「-」だけを使えます";
    const isAlphanumeric = (char) => /^[A-Za-z0-9]$/.test(char);
    const isFullWidth = (char) => char >= "！" && char <= "～";
    const isInvisible = (char) => /^[\p{Cc}\p{Cf}]$/u.test(char);
    const codePoint = (char) => `U+${char.codePointAt(0).toString(16).toUpperCase().padStart(4, "0")}`;

    function charProblem(char, position) {
        if (/^\s$/.test(char)) return `${position}文字目の空白は使用できません。${HINT}`;
        if (isInvisible(char)) return `${position}文字目に見えない文字（${codePoint(char)}）が含まれています。${HINT}`;
        const note = isFullWidth(char) ? "（全角文字です）" : "";
        return `${position}文字目の「${char}」は使用できません${note}。${HINT}`;
    }

    // Returns a message for the first problem, or "" when the ID is acceptable.
    // An empty value is left to the field's required attribute.
    function idProblem(value, { max = 64, reservedPrefix = "" } = {}) {
        const chars = Array.from(value.trim());
        for (const [index, char] of chars.entries()) {
            if (isAlphanumeric(char)) continue;
            if (char !== "_" && char !== "-") return charProblem(char, index + 1);
            if (index === 0) return "先頭は半角英数字にしてください";
        }
        if (chars.length > max) return `${max}文字以内で入力してください`;
        if (reservedPrefix && chars.join("").startsWith(reservedPrefix)) return `「${reservedPrefix}」で始まるIDは使用できません`;
        return "";
    }

    // Keeps the field's validity and its visible message in step with the value.
    // Returns the sync function for callers that assign the value from script.
    function bindIDField(input, options) {
        const feedback = document.createElement("div");
        feedback.id = `${input.id}Error`;
        feedback.className = "invalid-feedback";
        input.parentElement.append(feedback);
        const describedBy = input.getAttribute("aria-describedby");
        input.setAttribute("aria-describedby", describedBy ? `${describedBy} ${feedback.id}` : feedback.id);
        const sync = () => {
            const problem = idProblem(input.value, options);
            input.setCustomValidity(problem);
            input.classList.toggle("is-invalid", Boolean(problem));
            input.setAttribute("aria-invalid", String(Boolean(problem)));
            feedback.textContent = problem;
        };
        input.addEventListener("input", sync);
        // The reset event fires before the value is cleared.
        input.form?.addEventListener("reset", () => setTimeout(sync));
        return sync;
    }

    window.MinihubIDs = Object.freeze({ idProblem, bindIDField });
})();
