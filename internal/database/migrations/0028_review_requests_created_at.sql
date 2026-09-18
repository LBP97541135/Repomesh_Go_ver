-- 0028_review_requests_created_at.sql — humancontrol 读面补列。
-- 0009 建表遗漏:humancontrol 的 SELECT 引用 created_at,但表里只有
-- updated_at / decided_at,审核队列查询直接 500(internal)。
-- 幂等:IF NOT EXISTS;存量行以 updated_at 回填,保持行的历史时间语义。

ALTER TABLE public.review_requests
  ADD COLUMN IF NOT EXISTS created_at timestamptz NOT NULL DEFAULT now();

UPDATE public.review_requests SET created_at = updated_at;
