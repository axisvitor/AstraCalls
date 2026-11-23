import { test } from "node:test";
import assert from "node:assert/strict";
import { contactFromContext, parseAppContextMessage, samePhone } from "./chatwoot-context.ts";

const ATTRS = ["waha_whatsapp_jid", "waha_whatsapp_chat_id", "waha_whatsapp_lid"];
const ORIGIN = "https://chat.loja.com.br";
const msg = (data: unknown) => JSON.stringify({ event: "appContext", data });

test("telefone direto no contato", () => {
  const data = parseAppContextMessage(
    {
      origin: ORIGIN,
      data: msg({
        conversation: { id: 77, inbox_id: 12, meta: { sender: { name: "Outro" } } },
        contact: { name: "Ana", phone_number: "+55 94 99123-4567", custom_attributes: { waha_whatsapp_jid: "5511000000000@c.us" } },
        currentAgent: { id: 1, name: "Maria" },
      }),
    },
    [ORIGIN],
  );
  assert.deepEqual(contactFromContext(data, ATTRS), {
    name: "Ana",
    phone: "5594991234567",
    lid: "",
    inboxId: 12,
    conversationId: 77,
  });
});

test("só atributos do WAHA (@c.us e @s.whatsapp.net com aparelho)", () => {
  const c = contactFromContext(
    { conversation: { inbox_id: 3 }, contact: { name: "Bia", phone_number: null, custom_attributes: { waha_whatsapp_chat_id: "559491234567@c.us" } } },
    ATTRS,
  );
  assert.equal(c.phone, "559491234567");
  const d = contactFromContext(
    { contact: { custom_attributes: { waha_whatsapp_jid: "5594991234567:12@s.whatsapp.net" } } },
    ATTRS,
  );
  assert.equal(d.phone, "5594991234567");
});

test("só @lid: sem telefone, guarda o LID para o servidor resolver", () => {
  const c = contactFromContext({ contact: { custom_attributes: { waha_whatsapp_lid: "123456789012345@lid" } } }, ATTRS);
  assert.equal(c.phone, "");
  assert.equal(c.lid, "123456789012345@lid");
});

test("contato só em conversation.meta.sender (sem data.contact)", () => {
  const c = contactFromContext({ conversation: { inbox_id: 5, meta: { sender: { name: "Caio", phone_number: "+5511988887777" } } } }, ATTRS);
  assert.equal(c.phone, "5511988887777");
  assert.equal(c.inboxId, 5);
});

test("sem nada utilizável", () => {
  for (const data of [null, {}, { contact: {} }, { contact: { phone_number: "123", custom_attributes: { waha_whatsapp_jid: "1203630@g.us" } } }]) {
    const c = contactFromContext(data, ATTRS);
    assert.equal(c.phone, "");
    assert.equal(c.lid, "");
  }
});

test("lista de atributos configurável e em ordem", () => {
  const custom = { meu_numero: "5511911112222", waha_whatsapp_jid: "5511933334444@c.us" };
  assert.equal(contactFromContext({ contact: { custom_attributes: custom } }, ["meu_numero", ...ATTRS]).phone, "5511911112222");
  assert.equal(contactFromContext({ contact: { custom_attributes: custom } }, ATTRS).phone, "5511933334444");
});

test("mensagens malformadas ou de origem não permitida são ignoradas", () => {
  const good = msg({ contact: { phone_number: "+5511988887777" } });
  const parent = {};
  assert.equal(parseAppContextMessage({ origin: "https://evil.example", data: good }, [ORIGIN]), null);
  assert.equal(parseAppContextMessage({ origin: ORIGIN, data: good, source: {} }, [ORIGIN], parent), null);
  assert.equal(parseAppContextMessage({ origin: ORIGIN, data: "{não é json" }, [ORIGIN]), null);
  assert.equal(parseAppContextMessage({ origin: ORIGIN, data: { event: "appContext", data: {} } }, [ORIGIN]), null);
  assert.equal(parseAppContextMessage({ origin: ORIGIN, data: JSON.stringify({ event: "outro", data: {} }) }, [ORIGIN]), null);
  assert.equal(parseAppContextMessage({ origin: ORIGIN, data: JSON.stringify(["appContext"]) }, [ORIGIN]), null);
  assert.equal(parseAppContextMessage({ origin: ORIGIN, data: good }, []), null);
  assert.notEqual(parseAppContextMessage({ origin: ORIGIN + "/", data: good, source: parent }, [ORIGIN], parent), null);
});

test("mesmo telefone com e sem 9º dígito", () => {
  assert.ok(samePhone("5594991234567", "559491234567"));
  assert.ok(samePhone("+55 (94) 99123-4567", "5594991234567@s.whatsapp.net".split("@")[0]));
  assert.ok(!samePhone("5594991234567", "5594991234568"));
  assert.ok(!samePhone("", ""));
});
