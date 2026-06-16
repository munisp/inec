-- KOH 2027 Indicators — Seed Data
-- Populates all KOH tables with realistic campaign performance data.

BEGIN;

-- ─── CPI History (6 months of monthly readings) ────────────────────────────

INSERT INTO gotv_cpi_history (party_id, computed_at, cpi_score, voting_intention_pct, favourability_pct, digital_sentiment, ground_mobilisation, endorsement_index, share_of_voice)
VALUES
    (1, NOW() - INTERVAL '6 months', 42.5, 38.0, 45.0, 50.0, 25.0, 15.0, 22.0),
    (1, NOW() - INTERVAL '5 months', 48.2, 42.0, 48.0, 52.0, 35.0, 22.0, 28.0),
    (1, NOW() - INTERVAL '4 months', 53.8, 46.0, 52.0, 55.0, 42.0, 35.0, 32.0),
    (1, NOW() - INTERVAL '3 months', 58.1, 50.0, 55.0, 58.0, 48.0, 42.0, 35.0),
    (1, NOW() - INTERVAL '2 months', 62.4, 54.0, 58.0, 62.0, 55.0, 48.0, 38.0),
    (1, NOW() - INTERVAL '1 month', 65.7, 58.0, 62.0, 65.0, 60.0, 55.0, 42.0)
ON CONFLICT DO NOTHING;

-- ─── Surveys (3 waves of field research) ────────────────────────────────────

INSERT INTO gotv_surveys (party_id, survey_name, wave_number, sample_size, methodology, start_date, end_date, status) VALUES
    (1, 'KOH Lagos Baseline Survey', 1, 1500, 'Quota sampling by LGA/gender/age/class', '2026-01-15', '2026-01-25', 'completed'),
    (1, 'KOH Lagos Wave 2 - Post Campaign Launch', 2, 1800, 'Quota sampling by LGA/gender/age/class', '2026-03-01', '2026-03-12', 'completed'),
    (1, 'KOH Lagos Wave 3 - Mid-Campaign', 3, 2000, 'Quota sampling by LGA/gender/age/class', '2026-05-15', '2026-05-28', 'active')
ON CONFLICT DO NOTHING;

-- Wave 1 responses (sample across demographics)
INSERT INTO gotv_survey_responses (survey_id, party_id, lga_code, age_group, gender, socioeconomic_class, education_level, awareness_score, favourability_score, voting_intention, issue_alignment, nps_score, net_promoter)
SELECT 
    1, 1,
    (ARRAY['alimosho','mushin','agege','kosofe','somolu','ikorodu','ikeja','eti-osa','surulere','ojo'])[floor(random()*10)+1],
    (ARRAY['youth_18_35','middle_36_55','senior_56_plus'])[floor(random()*3)+1],
    (ARRAY['male','female'])[floor(random()*2)+1],
    (ARRAY['AB','C1','C2','DE'])[floor(random()*4)+1],
    (ARRAY['tertiary','secondary','primary','none'])[floor(random()*4)+1],
    40 + random()*40,  -- awareness 40-80
    35 + random()*35,  -- favourability 35-70
    random() > 0.55,   -- 45% voting intention
    30 + random()*40,  -- issue alignment
    floor(random()*10)+1,
    CASE WHEN random() > 0.7 THEN 'promoter' WHEN random() > 0.3 THEN 'passive' ELSE 'detractor' END
FROM generate_series(1, 200);

-- Wave 2 responses (improved numbers post-launch)
INSERT INTO gotv_survey_responses (survey_id, party_id, lga_code, age_group, gender, socioeconomic_class, education_level, awareness_score, favourability_score, voting_intention, issue_alignment, nps_score, net_promoter)
SELECT
    2, 1,
    (ARRAY['alimosho','mushin','agege','kosofe','somolu','ikorodu','ikeja','eti-osa','surulere','ojo','badagry','epe'])[floor(random()*12)+1],
    (ARRAY['youth_18_35','middle_36_55','senior_56_plus'])[floor(random()*3)+1],
    (ARRAY['male','female'])[floor(random()*2)+1],
    (ARRAY['AB','C1','C2','DE'])[floor(random()*4)+1],
    (ARRAY['tertiary','secondary','primary','none'])[floor(random()*4)+1],
    50 + random()*35,  -- awareness 50-85
    42 + random()*35,  -- favourability 42-77
    random() > 0.45,   -- 55% voting intention
    35 + random()*40,
    floor(random()*10)+1,
    CASE WHEN random() > 0.6 THEN 'promoter' WHEN random() > 0.3 THEN 'passive' ELSE 'detractor' END
FROM generate_series(1, 300);

-- Wave 3 responses (further improvement)
INSERT INTO gotv_survey_responses (survey_id, party_id, lga_code, age_group, gender, socioeconomic_class, education_level, awareness_score, favourability_score, voting_intention, issue_alignment, nps_score, net_promoter)
SELECT
    3, 1,
    (ARRAY['alimosho','mushin','agege','kosofe','somolu','ikorodu','ikeja','eti-osa','surulere','ojo','badagry','epe','ibeju-lekki','lagos-mainland','ifako-ijaiye'])[floor(random()*15)+1],
    (ARRAY['youth_18_35','middle_36_55','senior_56_plus'])[floor(random()*3)+1],
    (ARRAY['male','female'])[floor(random()*2)+1],
    (ARRAY['AB','C1','C2','DE'])[floor(random()*4)+1],
    (ARRAY['tertiary','secondary','primary','none'])[floor(random()*4)+1],
    55 + random()*35,  -- awareness 55-90
    48 + random()*35,  -- favourability 48-83
    random() > 0.38,   -- 62% voting intention
    40 + random()*40,
    floor(random()*10)+1,
    CASE WHEN random() > 0.55 THEN 'promoter' WHEN random() > 0.25 THEN 'passive' ELSE 'detractor' END
FROM generate_series(1, 400);

-- ─── Social Metrics (daily data for past 30 days) ───────────────────────────

INSERT INTO gotv_social_metrics (party_id, date, platform, metric_type, value)
SELECT
    1,
    CURRENT_DATE - (s.d || ' days')::interval,
    p.platform,
    m.metric,
    CASE
        WHEN m.metric = 'sentiment_score' THEN 55 + random()*20
        WHEN m.metric = 'share_of_voice' THEN 28 + random()*15
        WHEN m.metric = 'mention_volume' THEN 500 + random()*2000
        WHEN m.metric = 'engagement_rate' THEN 2 + random()*5
    END
FROM generate_series(0, 29) AS s(d)
CROSS JOIN (VALUES ('twitter'), ('facebook'), ('instagram'), ('tiktok')) AS p(platform)
CROSS JOIN (VALUES ('sentiment_score'), ('share_of_voice'), ('mention_volume'), ('engagement_rate')) AS m(metric);

-- ─── Sentiment Log (500 mentions over past 30 days) ─────────────────────────

INSERT INTO gotv_sentiment_log (party_id, timestamp, platform, mention_text, sentiment, sentiment_score, candidate_mentioned, topic, url)
SELECT
    1,
    NOW() - (random()*30 || ' days')::interval,
    (ARRAY['twitter','facebook','instagram','news','tiktok'])[floor(random()*5)+1],
    (ARRAY[
        'Candidate has strong vision for Lagos infrastructure development',
        'This administration has failed Lagos youth employment',
        'The rally in Alimosho was massive! Great energy!',
        'Another promise maker. All politicians are the same',
        'Finally someone addressing the traffic situation on 3rd mainland bridge',
        'Women empowerment program is a game changer for Mushin market women',
        'Not convinced about the education policy. Needs more detail',
        'The endorsement from Alake of Egbaland speaks volumes',
        'Security situation in Ojo is concerning. What will be done?',
        'Healthcare plan seems promising for primary care centers'
    ])[floor(random()*10)+1],
    (ARRAY['positive','positive','positive','negative','negative','neutral','neutral','positive','negative','positive'])[floor(random()*10)+1],
    random(),
    'KOH Candidate',
    (ARRAY['infrastructure','employment','security','education','healthcare','economy','environment'])[floor(random()*7)+1],
    'https://social.example.com/post/' || floor(random()*100000)
FROM generate_series(1, 500);

-- ─── Endorsements (25 across various categories) ────────────────────────────

INSERT INTO gotv_endorsements (party_id, endorser_name, endorser_type, endorser_category, lga_code, demographic_reach, date_endorsed, public_statement, verified)
VALUES
    (1, 'Oba Akinwunmi of Ikeja', 'traditional_ruler', 'Lagos Traditional Council', 'ikeja', 50000, '2026-01-20', 'Our community will rally behind a leader who understands our heritage', true),
    (1, 'Chief Imam Alimosho Central Mosque', 'religious_leader', 'Islamic Council', 'alimosho', 80000, '2026-02-05', 'A candidate who promotes interfaith harmony', true),
    (1, 'Bishop Adeyemi - Pentecostal Fellowship', 'religious_leader', 'CAN', 'surulere', 120000, '2026-02-12', 'We need godly leadership in Lagos', true),
    (1, 'Lagos Chamber of Commerce', 'professional_body', 'Business', 'lagos-island', 200000, '2026-02-20', 'The economic blueprint is the most credible weve seen', true),
    (1, 'Nigerian Bar Association Lagos Branch', 'professional_body', 'Legal', 'ikeja', 50000, '2026-03-01', 'A candidate who respects rule of law', true),
    (1, 'National Union of Road Transport Workers', 'ethnic_union', 'Transport', 'mushin', 300000, '2026-03-05', 'Better roads, better lives for our members', true),
    (1, 'Federation of Igbo Women Lagos', 'womens_group', 'Ethnic Women', 'oshodi-isolo', 150000, '2026-03-10', 'Inclusive policies for all ethnic groups', true),
    (1, 'National Youth Council of Nigeria Lagos', 'youth_org', 'Youth Development', 'kosofe', 180000, '2026-03-15', 'Finally a candidate who talks TO youth not ABOUT youth', true),
    (1, 'Prof. Adesanya - UNILAG', 'academic', 'Higher Education', 'lagos-mainland', 30000, '2026-03-20', 'Research-driven policy making at last', true),
    (1, 'Alhaji Dangote (personal capacity)', 'celebrity', 'Business', 'ikoyi', 500000, '2026-04-01', 'I believe in this vision for Lagos', true),
    (1, 'Market Women Association Mushin', 'community_leader', 'Market', 'mushin', 100000, '2026-04-05', 'Lower market tolls and better drainage is what we need', true),
    (1, 'Ijaw National Congress Lagos', 'ethnic_union', 'Ethnic', 'ajeromi-ifelodun', 80000, '2026-04-10', 'Recognition of our communities matters', true),
    (1, 'Nigerian Medical Association Lagos', 'professional_body', 'Health', 'ikeja', 40000, '2026-04-15', 'Primary healthcare commitment is credible', true),
    (1, 'Former Senator Adekunle', 'politician', 'APC', 'alimosho', 200000, '2026-04-20', 'Experienced governance combined with fresh ideas', true),
    (1, 'Nollywood Stars Coalition', 'celebrity', 'Entertainment', 'surulere', 1000000, '2026-05-01', 'We endorse a creative economy vision', true),
    (1, 'Teachers Union of Nigeria Lagos', 'professional_body', 'Education', 'agege', 60000, '2026-05-05', 'Education funding commitment is unprecedented', true),
    (1, 'Artisan Guild of Badagry', 'community_leader', 'Artisans', 'badagry', 25000, '2026-05-10', 'Skills development programs will transform our youth', true),
    (1, 'Lagos Landlords Association', 'community_leader', 'Property', 'ikorodu', 150000, '2026-05-15', 'Property rights and land use reform needed', true),
    (1, 'Tech Hub Founders Collective', 'professional_body', 'Technology', 'eti-osa', 100000, '2026-05-20', 'Digital economy roadmap aligns with our vision', false),
    (1, 'National Council of Women Societies', 'womens_group', 'Women Umbrella', 'lagos-island', 250000, '2026-05-25', 'Gender equity in governance at last', true)
ON CONFLICT DO NOTHING;

-- ─── Defections (5 public defections to the party) ──────────────────────────

INSERT INTO gotv_defections (party_id, defector_name, from_party, to_party, defection_date, lga_code, is_public, description)
VALUES
    (1, 'Hon. Adebayo - Former LP Lawmaker', 'LP', 'APC', '2026-03-15', 'kosofe', true, 'Attracted by infrastructure development agenda'),
    (1, 'Chief Okafor - PDP Ward Chair Ojo', 'PDP', 'APC', '2026-04-01', 'ojo', true, 'Cited lack of vision in opposition party'),
    (1, 'Dr. Amina Bello - NNPP Women Leader', 'NNPP', 'APC', '2026-04-20', 'somolu', true, 'Womens empowerment program was the deciding factor'),
    (1, 'Engr. Okonkwo - LP Youth Coordinator', 'LP', 'APC', '2026-05-10', 'surulere', true, 'Youth employment blueprint more actionable than OBIdient promises'),
    (1, 'Alhaji Musa - PDP Councillor Agege', 'PDP', 'APC', '2026-05-28', 'agege', true, 'Community development track record speaks for itself')
ON CONFLICT DO NOTHING;

-- ─── Platform Analytics (daily metrics for Meta/TikTok/X/YouTube) ────────────

INSERT INTO gotv_platform_analytics (party_id, date, platform, followers, follower_growth_pct, total_reach, organic_reach, paid_reach, engagement_rate, video_completion_rate, impressions, clicks, shares, comments)
SELECT
    1,
    CURRENT_DATE - (s.d || ' days')::interval,
    p.platform,
    CASE p.platform
        WHEN 'facebook' THEN 450000 + s.d * 500
        WHEN 'instagram' THEN 280000 + s.d * 800
        WHEN 'twitter' THEN 350000 + s.d * 300
        WHEN 'tiktok' THEN 180000 + s.d * 1200
        WHEN 'youtube' THEN 95000 + s.d * 150
    END,
    CASE p.platform WHEN 'tiktok' THEN 2.5 + random()*2 ELSE 0.5 + random()*1.5 END,
    floor(50000 + random()*200000)::int,
    floor(30000 + random()*120000)::int,
    floor(20000 + random()*80000)::int,
    (2 + random()*4) / 100.0,
    (30 + random()*40) / 100.0,
    floor(100000 + random()*500000)::int,
    floor(2000 + random()*10000)::int,
    floor(500 + random()*5000)::int,
    floor(200 + random()*3000)::int
FROM generate_series(0, 29) AS s(d)
CROSS JOIN (VALUES ('facebook'), ('instagram'), ('twitter'), ('tiktok'), ('youtube')) AS p(platform);

-- ─── Generated Reports (5 historical reports) ───────────────────────────────

INSERT INTO gotv_reports_generated (party_id, report_type, frequency, data_snapshot, period_start, period_end, status)
VALUES
    (1, 'digital_performance', 'weekly', '{"total_reach_7d": 850000, "engagement_rate": 0.035, "top_platform": "tiktok"}', CURRENT_DATE - INTERVAL '14 days', CURRENT_DATE - INTERVAL '7 days', 'generated'),
    (1, 'full_indicators', 'monthly', '{"cpi": 62.4, "voting_intention": 54.0, "favourability": 58.0}', CURRENT_DATE - INTERVAL '60 days', CURRENT_DATE - INTERVAL '30 days', 'generated'),
    (1, 'cpi_brief', 'monthly', '{"cpi": 65.7, "interpretation": "competitive"}', CURRENT_DATE - INTERVAL '30 days', CURRENT_DATE, 'generated'),
    (1, 'demographic_sentiment', 'monthly', '{"youth_favourability": 62, "senior_favourability": 55, "gender_gap": -3}', CURRENT_DATE - INTERVAL '30 days', CURRENT_DATE, 'generated'),
    (1, 'digital_performance', 'weekly', '{"total_reach_7d": 920000, "engagement_rate": 0.038, "top_platform": "tiktok"}', CURRENT_DATE - INTERVAL '7 days', CURRENT_DATE, 'generated')
ON CONFLICT DO NOTHING;

-- ─── Update contacts with demographic data (assign LGA tiers to existing contacts) ─

UPDATE gotv_contacts SET
    lga_code = (ARRAY['alimosho','mushin','agege','kosofe','somolu','ikorodu','ikeja','eti-osa','surulere','ojo','badagry','epe','ibeju-lekki','lagos-mainland','ifako-ijaiye','oshodi-isolo','ajeromi-ifelodun','apapa','lagos-island'])[floor(random()*19)+1],
    age_group = (ARRAY['youth_18_35','middle_36_55','senior_56_plus'])[floor(random()*3)+1],
    gender = (ARRAY['male','female'])[floor(random()*2)+1],
    socioeconomic_class = (ARRAY['AB','C1','C2','DE'])[floor(random()*4)+1],
    occupation_group = (ARRAY['trader','civil_servant','artisan','professional','student','unemployed','farmer'])[floor(random()*7)+1],
    education_level = (ARRAY['tertiary','secondary','primary','none'])[floor(random()*4)+1],
    religion = (ARRAY['christianity','islam','traditional','other'])[floor(random()*4)+1],
    ethnicity = (ARRAY['yoruba','igbo','hausa','minority'])[floor(random()*4)+1]
WHERE party_id = 1 AND lga_code IS NULL;

-- Update LGA tiers based on assigned LGA codes
UPDATE gotv_contacts c SET lga_tier = t.tier
FROM gotv_lga_tiers t WHERE c.lga_code = t.lga_code AND c.party_id = 1;

COMMIT;
