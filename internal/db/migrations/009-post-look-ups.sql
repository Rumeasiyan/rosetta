-- What a post look-up leaves on a role (R-ADM-06; E-CAL-05). Checked is the last time BIFROST
-- answered about the person; flagged is since when they were answered as not found, or found at
-- no post, while their post is not known. A failed look-up touches neither.
ALTER TABLE role_grant ADD COLUMN post_checked_at timestamptz;
ALTER TABLE role_grant ADD COLUMN post_flagged_at timestamptz;
