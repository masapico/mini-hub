import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {runInNewContext} from 'node:vm';
import {test} from 'node:test';

const script = readFileSync(new URL('../web/js/id-validation.js', import.meta.url), 'utf8');
const context = {window: {}};
runInNewContext(script, context);
const {idProblem} = context.window.MinihubIDs;

test('accepts IDs the server accepts', () => {
    for (const id of ['a', 's_004012z', 'A-b_9', '0abc', 'a'.repeat(64), '  general  ', '']) {
        assert.equal(idProblem(id), '', id);
    }
});

test('names the position and the character that is not allowed', () => {
    assert.match(idProblem('ｓ_004012z'), /^1文字目の「ｓ」は使用できません（全角文字です）/);
    assert.match(idProblem('s＿004012z'), /^2文字目の「＿」は使用できません（全角文字です）/);
    assert.match(idProblem('s_00４012z'), /^5文字目の「４」は使用できません（全角文字です）/);
    assert.match(idProblem('相談'), /^1文字目の「相」は使用できません。/);
    assert.match(idProblem('a.b'), /^2文字目の「\.」は使用できません。/);
    assert.match(idProblem('a b'), /^2文字目の空白は使用できません/);
    assert.match(idProblem('a　b'), /^2文字目の空白は使用できません/);
});

test('counts surrogate pairs as one character', () => {
    assert.match(idProblem('a😀b'), /^2文字目の「😀」は使用できません。/);
    assert.match(idProblem('😀＿'), /^1文字目/);
});

test('shows characters that cannot be seen by their code point', () => {
    assert.match(idProblem('s_004012z​'), /^10文字目に見えない文字（U\+200B）が含まれています/);
    assert.match(idProblem('a\u0007b'), /^2文字目に見えない文字（U\+0007）が含まれています/);
});

test('requires an alphanumeric first character', () => {
    assert.equal(idProblem('_abc'), '先頭は半角英数字にしてください');
    assert.equal(idProblem('-abc'), '先頭は半角英数字にしてください');
});

test('enforces the length limit of each kind of ID', () => {
    assert.equal(idProblem('a'.repeat(65)), '64文字以内で入力してください');
    assert.equal(idProblem('a'.repeat(61), {max: 61}), '');
    assert.equal(idProblem('a'.repeat(62), {max: 61}), '61文字以内で入力してください');
});

test('rejects a reserved prefix only when configured', () => {
    assert.equal(idProblem('ai_bot', {reservedPrefix: 'ai_'}), '「ai_」で始まるIDは使用できません');
    assert.equal(idProblem('aide', {reservedPrefix: 'ai_'}), '');
    assert.equal(idProblem('ai_bot'), '');
});
