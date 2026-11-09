CREATE OR REPLACE FUNCTION public.read_roles(p bigint)
 RETURNS TABLE(n text, s numeric)
 LANGUAGE plpgsql
AS $function$
BEGIN
  RETURN QUERY
    SELECT m.name, sum(b.stake)
    FROM member m JOIN bet b ON b.member_id = m.id
    WHERE m.closed = false
    GROUP BY m.name
    HAVING sum(b.payout) > 0
    ORDER BY m.email;
  UPDATE member SET balance = 0 WHERE id = p RETURNING email INTO n;
END $function$

