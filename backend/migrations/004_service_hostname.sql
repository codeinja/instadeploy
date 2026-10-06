-- Remember each service's generated hostname, so making a service private
-- and then public again gives it back the same URL.
ALTER TABLE services ADD COLUMN hostname text NOT NULL DEFAULT '';

UPDATE services s SET hostname = r.hostname
FROM routes r WHERE r.service_id = s.id AND r.kind = 'GENERATED';
