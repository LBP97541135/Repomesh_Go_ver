/** Consume every page; reject cyclic cursors instead of silently truncating scope. */
export async function allPages<T>(read: (cursor?: string) => Promise<{ items: T[]; nextCursor: string | null }>): Promise<T[]> {
  const items: T[] = [];
  let cursor: string | undefined;
  const seen = new Set<string>();
  do {
    const page = await read(cursor);
    items.push(...page.items);
    cursor = page.nextCursor || undefined;
    if (cursor && seen.has(cursor)) throw new Error("分页游标重复，请刷新后重试。");
    if (cursor) seen.add(cursor);
  } while (cursor);
  return items;
}
