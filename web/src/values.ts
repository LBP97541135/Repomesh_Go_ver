const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/iu;
const controlPattern = /[\u0000-\u001f\u007f]/u;
const utcPattern = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?Z$/u;

export function scalarLength(value: string): number | null {
  let length = 0;
  for (let index = 0; index < value.length; index += 1) {
    const code = value.charCodeAt(index);
    if (code >= 0xd800 && code <= 0xdbff) {
      if (index + 1 >= value.length) return null;
      const next = value.charCodeAt(index + 1);
      if (next < 0xdc00 || next > 0xdfff) return null;
      index += 1;
    } else if (code >= 0xdc00 && code <= 0xdfff) {
      return null;
    }
    length += 1;
  }
  return length;
}

export function object(value: unknown): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) throw new Error("Invalid object");
  return Object.fromEntries(Object.entries(value));
}

export function text(value: unknown, maximum: number): string {
  if (typeof value !== "string") throw new Error("Invalid string");
  const length = scalarLength(value);
  if (length === null || length === 0 || length > maximum) throw new Error("Invalid string");
  return value;
}

export function isUuid(value: unknown): value is string {
  return typeof value === "string" && uuidPattern.test(value);
}

export function isSafeSegment(value: string): boolean {
  const length = scalarLength(value);
  return length !== null && length > 0 && length <= 128 && value !== "." && value !== ".." && !controlPattern.test(value) && !/[\\/%?#]/u.test(value);
}

export function identifier(value: unknown): string {
  const parsed = text(value, 128);
  if (!isSafeSegment(parsed)) throw new Error("Invalid identifier");
  return parsed;
}

export function timestamp(value: unknown): string {
  const parsed = text(value, 40);
  const match = utcPattern.exec(parsed);
  const milliseconds = Date.parse(parsed);
  if (match === null || !Number.isFinite(milliseconds)) throw new Error("Invalid timestamp");
  const date = new Date(milliseconds);
  if (date.getUTCFullYear() !== Number(match[1]) || date.getUTCMonth() + 1 !== Number(match[2]) || date.getUTCDate() !== Number(match[3]) || date.getUTCHours() !== Number(match[4]) || date.getUTCMinutes() !== Number(match[5]) || date.getUTCSeconds() !== Number(match[6])) throw new Error("Invalid timestamp");
  return parsed;
}

export function normalizeProjectOperationKey(value: string): string | null {
  return isUuid(value) ? value.toLowerCase() : null;
}
