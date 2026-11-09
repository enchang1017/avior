CREATE OR REPLACE PROCEDURE public.close_member(IN p_member bigint)
 LANGUAGE plpgsql
AS $procedure$
DECLARE r record;
BEGIN
  FOR r IN SELECT id FROM bet WHERE member_id = p_member AND status = 'open' LOOP
    PERFORM settle_bet(r.id, 0);
  END LOOP;
  DELETE FROM account WHERE member_id = p_member;
  UPDATE member SET closed = true WHERE id = p_member;
  COMMIT;
END $procedure$

