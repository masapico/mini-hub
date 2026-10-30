import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {runInNewContext} from 'node:vm';
import {test} from 'node:test';

const script = readFileSync(new URL('../web/js/ai-canvas.js', import.meta.url), 'utf8');

function payloadMessages() {
    // readyState "loading" defers init, so only the exposed helpers are evaluated.
    const context = {
        window: {},
        document: {readyState: 'loading', addEventListener() {}, getElementById() { return null; }},
    };
    runInNewContext(script, context);
    return context.window.aiCanvasPayloadMessages;
}

test('AI canvas payload drops failed turns and keeps alternating roles', () => {
    const build = payloadMessages();
    const got = build([
        {role: 'user', content: 'first'},
        {role: 'assistant', content: 'answer'},
        {role: 'user', content: 'failed question'},
        {role: 'assistant', content: '⚠️ AI応答エラー', error: true},
        {role: 'user', content: 'next question'},
        {role: 'assistant', thinking: true},
    ]);
    assert.deepEqual(JSON.parse(JSON.stringify(got)), [
        {role: 'user', content: 'first'},
        {role: 'assistant', content: 'answer'},
        {role: 'user', content: 'next question'},
    ]);
});

test('AI canvas payload tags earlier attachments and keeps the current attachment-only message', () => {
    const build = payloadMessages();
    const got = build([
        {role: 'user', content: '', attachments: [{name: 'a.pdf'}, {name: 'b.xlsx'}]},
        {role: 'assistant', content: 'read'},
        {role: 'user', content: '要約して', attachments: [{name: 'c.docx'}]},
        {role: 'assistant', content: 'summary'},
        {role: 'user', content: '', attachments: [{name: 'd.csv'}]},
    ]);
    assert.deepEqual(JSON.parse(JSON.stringify(got)), [
        {role: 'user', content: '[添付ファイル: a.pdf]\n[添付ファイル: b.xlsx]'},
        {role: 'assistant', content: 'read'},
        {role: 'user', content: '要約して\n[添付ファイル: c.docx]'},
        {role: 'assistant', content: 'summary'},
        {role: 'user', content: ''},
    ]);
});

test('AI canvas payload sends at most the last 10 messages', () => {
    const build = payloadMessages();
    const messages = Array.from({length: 15}, (_, i) => ({role: i % 2 ? 'assistant' : 'user', content: `m${i}`}));
    const got = build(messages);
    assert.equal(got.length, 10);
    assert.equal(got[0].content, 'm5');
    assert.equal(got[9].content, 'm14');
});
