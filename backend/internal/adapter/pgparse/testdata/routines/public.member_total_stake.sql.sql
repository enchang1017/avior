CREATE OR REPLACE FUNCTION public.member_total_stake(p_member bigint)
 RETURNS numeric
 LANGUAGE sql
 STABLE
BEGIN ATOMIC
 SELECT COALESCE(sum(b.stake), (0)::numeric) AS "coalesce"
    FROM bet b
   WHERE (b.member_id = member_total_stake.p_member);
END

