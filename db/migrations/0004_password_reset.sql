-- 密码重置之后要能吊销「别处的会话」，所以要能按 sub 批量吊销。
-- 索引本来就有（ix_web_sessions_sub），但那是部分索引（只含未吊销的行），
-- 恰好就是吊销要扫的范围，不需要新索引。
--
-- 这一条迁移只做一件事：给 credentials 记一个「上次换密码的时刻」。
-- 用途不是展示，而是「改密码之后，比这个时刻更早签发的会话一律失效」——
-- 比逐个 DELETE 更稳（漏删一个就是一个永久后门），也能在怀疑泄露时一次性处理。
ALTER TABLE credentials ADD COLUMN rotated_at timestamptz;

-- 已有密码凭证视作「从未轮换」：NULL 表示「不限签发时刻」，
-- 这样上线瞬间不会把所有在线用户踢掉。
COMMENT ON COLUMN credentials.rotated_at IS
    '密码上次变更时刻。非空时，web_sessions.created_at 早于它的会话一律失效（NULL = 不作废任何会话）。';
