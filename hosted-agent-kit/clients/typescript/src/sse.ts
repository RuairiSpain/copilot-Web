/** A small server-sent-events parser. */

export interface SseEvent {
  event: string;
  /** Parsed JSON when the data is JSON, otherwise the text. */
  data: unknown;
  raw: string;
}

function decode(raw: string): unknown {
  try {
    return JSON.parse(raw);
  } catch {
    return raw;
  }
}

/** Yield events from decoded text chunks. Chunks may split lines and events anywhere. */
export async function* parseSse(chunks: AsyncIterable<string>): AsyncGenerator<SseEvent> {
  let buffer = "";
  let event = "message";
  let data: string[] = [];

  const feed = (rawLine: string): SseEvent | undefined => {
    const line = rawLine.endsWith("\r") ? rawLine.slice(0, -1) : rawLine;
    if (line === "") {
      let finished: SseEvent | undefined;
      if (data.length > 0) {
        const raw = data.join("\n");
        finished = { event, data: decode(raw), raw };
      }
      event = "message";
      data = [];
      return finished;
    }
    if (line.startsWith(":")) return undefined; // a comment
    const colon = line.indexOf(":");
    const name = colon < 0 ? line : line.slice(0, colon);
    let value = colon < 0 ? "" : line.slice(colon + 1);
    if (value.startsWith(" ")) value = value.slice(1);
    if (name === "event") event = value;
    else if (name === "data") data.push(value);
    return undefined;
  };

  for await (const chunk of chunks) {
    buffer += chunk;
    let newline = buffer.indexOf("\n");
    while (newline >= 0) {
      const line = buffer.slice(0, newline);
      buffer = buffer.slice(newline + 1);
      const finished = feed(line);
      if (finished) yield finished;
      newline = buffer.indexOf("\n");
    }
  }
  // The stream ended without a final newline or blank line.
  if (buffer) {
    const finished = feed(buffer);
    if (finished) yield finished;
  }
  const last = feed("");
  if (last) yield last;
}
