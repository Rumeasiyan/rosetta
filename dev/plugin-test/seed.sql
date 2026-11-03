-- The invented system of dev/tva/systems.json, with the editor's address listed.
INSERT INTO caller (tva_reference, name, owner_subject, tva_status, tva_checked_at)
VALUES ('TEST-0001', 'Test letter editor', 'dev-owner', 'live', now())
ON CONFLICT (tva_reference) DO NOTHING;
INSERT INTO caller_origin (caller_id, origin)
SELECT id, 'https://writedocument.epc.test' FROM caller WHERE tva_reference = 'TEST-0001'
ON CONFLICT DO NOTHING;
