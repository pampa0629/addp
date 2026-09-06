/*
 * ADDP RTF text parser.
 * SPDX-License-Identifier: MIT
 */

type RtfState = {
  codePage: number;
  hidden: boolean;
  skip: boolean;
  unicodeFallbackLength: number;
};

const skippedDestinations = new Set([
  "annotation",
  "author",
  "colortbl",
  "comment",
  "creatim",
  "datastore",
  "doccomm",
  "filetbl",
  "fonttbl",
  "footer",
  "footerf",
  "footerl",
  "footerr",
  "generator",
  "header",
  "headerf",
  "headerl",
  "headerr",
  "info",
  "keywords",
  "latentstyles",
  "listoverridetable",
  "listtable",
  "manager",
  "nextfile",
  "object",
  "operator",
  "pict",
  "private",
  "revtbl",
  "rsidtbl",
  "stylesheet",
  "subject",
  "template",
  "title",
  "themedata",
  "xmlnstbl"
]);

const controlText: Record<string, string> = {
  bullet: "•",
  cell: "\t",
  emdash: "—",
  emspace: "\u2003",
  endash: "–",
  enspace: "\u2002",
  line: "\n",
  lquote: "‘",
  ldblquote: "“",
  page: "\n",
  par: "\n",
  pard: "\n",
  qmspace: "\u2005",
  quote: "’",
  rquote: "’",
  rdblquote: "”",
  row: "\n",
  tab: "\t"
};

const codePageLabels: Record<number, string> = {
  874: "windows-874",
  932: "shift_jis",
  936: "gb18030",
  949: "euc-kr",
  950: "big5",
  1200: "utf-16le",
  1201: "utf-16be",
  1250: "windows-1250",
  1251: "windows-1251",
  1252: "windows-1252",
  1253: "windows-1253",
  1254: "windows-1254",
  1255: "windows-1255",
  1256: "windows-1256",
  1257: "windows-1257",
  1258: "windows-1258",
  65001: "utf-8"
};

export function extractRtfText(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  const fontCodePages = detectFontCodePages(bytes);
  const states: RtfState[] = [{ codePage: detectAnsiCodePage(bytes), hidden: false, skip: false, unicodeFallbackLength: 1 }];
  const output: string[] = [];
  let fallbackCharactersToSkip = 0;
  let pendingBytes: number[] = [];
  let pendingCodePage = states[0].codePage;

  const state = () => states[states.length - 1];
  const flushBytes = () => {
    if (pendingBytes.length > 0) {
      if (!state().skip && !state().hidden) {
        output.push(decodeBytes(pendingBytes, pendingCodePage));
      }
      pendingBytes = [];
    }
  };
  const appendText = (value: string) => {
    flushBytes();
    if (!state().skip && !state().hidden) output.push(value);
  };
  const appendByte = (value: number) => {
    if (state().skip || state().hidden) return;
    if (pendingBytes.length > 0 && pendingCodePage !== state().codePage) flushBytes();
    pendingCodePage = state().codePage;
    pendingBytes.push(value);
  };

  for (let index = 0; index < bytes.length; index += 1) {
    const byte = bytes[index];

    if (byte === 0x7b) {
      flushBytes();
      states.push({ ...state() });
      continue;
    }
    if (byte === 0x7d) {
      flushBytes();
      if (states.length > 1) states.pop();
      continue;
    }
    if (byte !== 0x5c) {
      if (fallbackCharactersToSkip > 0) {
        fallbackCharactersToSkip -= 1;
      } else if (byte >= 0x80) {
        appendByte(byte);
      } else {
        flushBytes();
        if (byte !== 0x0a && byte !== 0x0d && !state().skip && !state().hidden) {
          output.push(String.fromCharCode(byte));
        }
      }
      continue;
    }

    const next = bytes[index + 1];
    if (next === undefined) break;

    if (next === 0x27 && isHexByte(bytes[index + 2]) && isHexByte(bytes[index + 3])) {
      const value = Number.parseInt(String.fromCharCode(bytes[index + 2], bytes[index + 3]), 16);
      if (fallbackCharactersToSkip > 0) fallbackCharactersToSkip -= 1;
      else appendByte(value);
      index += 3;
      continue;
    }

    flushBytes();
    if (next === 0x5c || next === 0x7b || next === 0x7d) {
      if (fallbackCharactersToSkip > 0) fallbackCharactersToSkip -= 1;
      else if (!state().skip && !state().hidden) output.push(String.fromCharCode(next));
      index += 1;
      continue;
    }
    if (next === 0x7e) {
      appendText("\u00a0");
      index += 1;
      continue;
    }
    if (next === 0x5f) {
      appendText("‑");
      index += 1;
      continue;
    }
    if (next === 0x2d) {
      appendText("\u00ad");
      index += 1;
      continue;
    }
    if (next === 0x2a) {
      state().skip = true;
      index += 1;
      continue;
    }
    if (next === 0x0a || next === 0x0d) {
      appendText("\n");
      index += next === 0x0d && bytes[index + 2] === 0x0a ? 2 : 1;
      continue;
    }
    if (!isAsciiLetter(next)) {
      index += 1;
      continue;
    }

    let cursor = index + 1;
    while (cursor < bytes.length && isAsciiLetter(bytes[cursor])) cursor += 1;
    const word = ascii(bytes.subarray(index + 1, cursor));
    let sign = 1;
    if (bytes[cursor] === 0x2d) {
      sign = -1;
      cursor += 1;
    }
    const numberStart = cursor;
    while (cursor < bytes.length && isAsciiDigit(bytes[cursor])) cursor += 1;
    const parameter = cursor > numberStart
      ? sign * Number.parseInt(ascii(bytes.subarray(numberStart, cursor)), 10)
      : undefined;
    if (bytes[cursor] === 0x20) cursor += 1;
    index = cursor - 1;

    if (skippedDestinations.has(word)) {
      state().skip = true;
      continue;
    }
    if (word === "ansicpg" && parameter !== undefined) {
      state().codePage = parameter;
      continue;
    }
    if (word === "f" && parameter !== undefined) {
      state().codePage = fontCodePages.get(parameter) || state().codePage;
      continue;
    }
    if (word === "uc" && parameter !== undefined) {
      state().unicodeFallbackLength = Math.max(0, parameter);
      continue;
    }
    if (word === "u" && parameter !== undefined) {
      appendText(String.fromCharCode(parameter < 0 ? parameter + 0x10000 : parameter));
      fallbackCharactersToSkip = state().unicodeFallbackLength;
      continue;
    }
    if (word === "bin" && parameter !== undefined && parameter > 0) {
      index += parameter;
      continue;
    }
    if (word === "v") {
      state().hidden = parameter !== 0;
      continue;
    }
    const replacement = controlText[word];
    if (replacement !== undefined) appendText(replacement);
  }

  flushBytes();
  return output
    .join("")
    .replace(/\r/g, "")
    .replace(/[ \t]+\n/g, "\n")
    .replace(/\n{3,}/g, "\n\n")
    .trim();
}

function detectAnsiCodePage(bytes: Uint8Array): number {
  const header = ascii(bytes.subarray(0, Math.min(bytes.length, 4096)));
  const match = header.match(/\\ansicpg(\d+)/i);
  return match ? Number.parseInt(match[1], 10) : 1252;
}

function detectFontCodePages(bytes: Uint8Array): Map<number, number> {
  const header = ascii(bytes.subarray(0, Math.min(bytes.length, 16384)));
  const fontCodePages = new Map<number, number>();
  for (const match of header.matchAll(/\\f(\d+)(?:(?![{};]).)*?\\fcharset(\d+)/gi)) {
    fontCodePages.set(Number.parseInt(match[1], 10), codePageForFontCharset(Number.parseInt(match[2], 10)));
  }
  return fontCodePages;
}

function codePageForFontCharset(charset: number): number {
  const codePages: Record<number, number> = {
    0: 1252,
    77: 10000,
    128: 932,
    129: 949,
    134: 936,
    136: 950,
    161: 1253,
    162: 1254,
    163: 1258,
    177: 1255,
    178: 1256,
    186: 1257,
    204: 1251,
    222: 874,
    238: 1250
  };
  return codePages[charset] || 1252;
}

function decodeBytes(values: number[], codePage: number): string {
  if (codePage === 1252) return decodeWindows1252(values);
  const label = codePageLabels[codePage] || "windows-1252";
  try {
    return new TextDecoder(label, { fatal: true }).decode(new Uint8Array(values));
  } catch {
    return decodeWindows1252(values);
  }
}

function decodeWindows1252(values: number[]): string {
  const replacements: Record<number, string> = {
    0x80: "€", 0x82: "‚", 0x83: "ƒ", 0x84: "„", 0x85: "…", 0x86: "†", 0x87: "‡",
    0x88: "ˆ", 0x89: "‰", 0x8a: "Š", 0x8b: "‹", 0x8c: "Œ", 0x8e: "Ž", 0x91: "‘",
    0x92: "’", 0x93: "“", 0x94: "”", 0x95: "•", 0x96: "–", 0x97: "—", 0x98: "˜",
    0x99: "™", 0x9a: "š", 0x9b: "›", 0x9c: "œ", 0x9e: "ž", 0x9f: "Ÿ"
  };
  return values.map((value) => replacements[value] || String.fromCharCode(value)).join("");
}

function ascii(bytes: Uint8Array): string {
  let value = "";
  for (const byte of bytes) value += String.fromCharCode(byte);
  return value;
}

function isAsciiLetter(value: number | undefined): boolean {
  return value !== undefined && ((value >= 0x41 && value <= 0x5a) || (value >= 0x61 && value <= 0x7a));
}

function isAsciiDigit(value: number | undefined): boolean {
  return value !== undefined && value >= 0x30 && value <= 0x39;
}

function isHexByte(value: number | undefined): boolean {
  return value !== undefined && (isAsciiDigit(value) || (value >= 0x41 && value <= 0x46) || (value >= 0x61 && value <= 0x66));
}
