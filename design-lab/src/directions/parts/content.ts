// Invented sample content for judging the look. Sample translations, not checked by a keeper.
export const term = {
  type: 'Post',
  en: 'Chief Secretary',
  si: 'ප්‍රධාන ලේකම්',
  ta: 'பிரதம செயலாளர்',
  note: 'Head of the provincial public service.',
  version: 'Glossary version 214',
}
export const langs = [
  { code: 'si', name: 'සිංහල', keeper: 'Keeper of Sinhala', when: 'checked' },
  { code: 'ta', name: 'தமிழ்', keeper: 'Keeper of Tamil', when: 'checked' },
  { code: 'en', name: 'English', keeper: 'Keeper of English', when: 'checked' },
] as const
export const terms = [
  { en: 'Chief Secretary', si: 'ප්‍රධාන ලේකම්', ta: 'பிரதம செயலாளர்', type: 'Post', status: 'Published' },
  { en: 'Eastern Province', si: 'නැගෙනහිර පළාත', ta: 'கிழக்கு மாகாணம்', type: 'Place', status: 'Published' },
  { en: 'Provincial Council', si: 'පළාත් සභාව', ta: 'மாகாண சபை', type: 'Institution', status: 'Published' },
  { en: 'Divisional Secretariat', si: 'ප්‍රාදේශීය ලේකම් කාර්යාලය', ta: 'பிரதேச செயலகம்', type: 'Institution', status: 'Held: Tamil not checked' },
  { en: 'SCOS Pro', si: 'SCOS Pro', ta: 'SCOS Pro', type: 'Keep as written', status: 'Published' },
]
export const recheck = {
  from: 'The Chief Secretary will visit the office on Monday.',
  to: 'பிரதம செயலாளர் திங்கட்கிழமை அலுவலகத்திற்கு வருகை தருவார்.',
  why: 'Holds a term changed in glossary version 214',
  by: 'Approved by Assistant Secretary, in SCOS Pro',
  left: '12 more to recheck',
}
export const sources = [
  { name: 'google-translate', state: 'On', note: 'Open for all six directions' },
  { name: 'grammar-site-a', state: 'On', note: 'English score 72' },
  { name: 'grammar-site-b', state: 'Resting', note: 'A block. Tried again in 40 minutes' },
]
export const today = [
  { label: 'Requests', value: '1,204' },
  { label: 'From glossary or memory', value: '61%' },
  { label: 'Failed', value: '9' },
]
export const reasons = [
  ['No provider', '5'],
  ['Over limit', '3'],
  ['Masking lost', '1'],
] as const
export const nav = ['Glossary', 'Memory', 'Status', 'Usage', 'Settings']
