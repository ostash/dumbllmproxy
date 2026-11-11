// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

import { JSONView } from "./JSONView";
import { CopyButton } from "./CopyButton";

type Props = {
  requestBody?: string;
  responseBody?: string;
  streamed: boolean;
  rawResponseBody?: string;
};

export function PayloadView({ requestBody, responseBody, streamed, rawResponseBody }: Props) {
  return (
    <>
      <section>
        <h3>Request body</h3>
        <PayloadBody value={requestBody} pretty={isChatRequest(requestBody)} />
      </section>
      <section>
        <h3>{streamed ? "Reconstructed final response" : "Response body"}</h3>
        <PayloadBody value={responseBody} pretty={isChatResponse(responseBody)} />
      </section>
      {rawResponseBody && (
        <section>
          <h3>Fallback raw decoded stream</h3>
          <RawPayload value={rawResponseBody} />
        </section>
      )}
    </>
  );
}

function PayloadBody({ value, pretty }: { value?: string; pretty: boolean }) {
  if (pretty) {
    return <JSONView value={value} />;
  }
  return <RawPayload value={value} />;
}

function RawPayload({ value }: { value?: string }) {
  return (
    <div className="payload-frame">
      <CopyButton text={value} />
      <pre className="payload">{value || "—"}</pre>
    </div>
  );
}

function isChatRequest(value?: string): boolean {
  if (!value) return false;
  try {
    const parsed = JSON.parse(value) as { messages?: unknown };
    return Array.isArray(parsed.messages);
  } catch {
    return false;
  }
}

function isChatResponse(value?: string): boolean {
  if (!value) return false;
  try {
    const parsed = JSON.parse(value) as { choices?: unknown };
    return Array.isArray(parsed.choices);
  } catch {
    return false;
  }
}
