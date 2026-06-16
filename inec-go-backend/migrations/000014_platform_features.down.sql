-- Rollback: Platform: MFA, SMS/USSD, training, registration

DROP TABLE IF EXISTS training_vr_scenarios CASCADE;
DROP TABLE IF EXISTS training_enrollments CASCADE;
DROP TABLE IF EXISTS training_courses CASCADE;
DROP TABLE IF EXISTS training_certificates CASCADE;
DROP TABLE IF EXISTS registration_centers CASCADE;
DROP TABLE IF EXISTS ussd_sessions CASCADE;
DROP TABLE IF EXISTS sms_verifications CASCADE;
DROP TABLE IF EXISTS sms_delivery_log CASCADE;
DROP TABLE IF EXISTS mfa_webauthn CASCADE;
DROP TABLE IF EXISTS mfa_totp CASCADE;
DROP TABLE IF EXISTS mfa_sms_otp CASCADE;
DROP TABLE IF EXISTS mfa_settings CASCADE;

