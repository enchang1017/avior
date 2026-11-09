CREATE OR REPLACE FUNCTION public.merge_and_into()
 RETURNS void
 LANGUAGE plpgsql
AS $function$
BEGIN
  MERGE INTO account a USING member m ON a.member_id = m.id
    WHEN MATCHED THEN UPDATE SET currency = 'THB';
  CREATE TABLE snapshot_bet AS SELECT id, stake FROM bet;
END $function$

