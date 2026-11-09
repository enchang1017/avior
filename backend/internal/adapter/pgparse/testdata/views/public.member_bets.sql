 SELECT m.id AS member_id,
    m.name,
    b.id AS bet_id,
    b.stake,
    b.status
   FROM (member m
     JOIN bet b ON ((b.member_id = m.id)));
