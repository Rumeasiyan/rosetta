package i18n

// messages is every word on the screens. Sinhala and Tamil are best effort, to be
// checked by a person before Go live (docs/DECISION-LOG.md).
var messages = map[string]text{
	"rosetta":    {"ROSETTA", "ROSETTA රොසෙටා", "ROSETTA ரொசெட்டா"},
	"skip":       {"Skip to the content", "අන්තර්ගතයට යන්න", "உள்ளடக்கத்திற்குச் செல்க"},
	"tagline":    {"Translation and grammar for every provincial system", "සෑම පළාත් පද්ධතියකටම පරිවර්තනය සහ ව්‍යාකරණ", "ஒவ்வொரு மாகாண முறைமைக்கும் மொழிபெயர்ப்பும் இலக்கணமும்"},
	"signIn":     {"Sign in", "පිවිසෙන්න", "உள்நுழைக"},
	"signOut":    {"Sign out", "පිටවන්න", "வெளியேறுக"},
	"signInNote": {"Sign in with your provincial account.", "ඔබේ පළාත් ගිණුමෙන් පිවිසෙන්න.", "உங்கள் மாகாண கணக்கினால் உள்நுழைக."},

	"signInFailed":      {"The sign-in did not finish. Please try again.", "පිවිසීම අවසන් වූයේ නැත. නැවත උත්සාහ කරන්න.", "உள்நுழைவு நிறைவடையவில்லை. மீண்டும் முயற்சிக்கவும்."},
	"signInUnavailable": {"The sign-in service is not answering. Your work is safe: please try again in a moment.", "පිවිසුම් සේවාව පිළිතුරු නොදේ. ඔබේ වැඩ ආරක්ෂිතයි: මොහොතකින් නැවත උත්සාහ කරන්න.", "உள்நுழைவுச் சேவை பதிலளிக்கவில்லை. உங்கள் பணி பாதுகாப்பாக உள்ளது: சிறிது நேரத்தில் மீண்டும் முயற்சிக்கவும்."},
	"signedOut":         {"You have signed out of ROSETTA.", "ඔබ ROSETTA වෙතින් පිටව ඇත.", "நீங்கள் ROSETTA இலிருந்து வெளியேறிவிட்டீர்கள்."},
	"expired":           {"Your session has ended. Please sign in again.", "ඔබේ සැසිය අවසන් වී ඇත. නැවත පිවිසෙන්න.", "உங்கள் அமர்வு முடிந்தது. மீண்டும் உள்நுழைக."},
	"badForm":           {"That form has expired. Please go back and try again.", "එම පෝරමය කල් ඉකුත් වී ඇත. ආපසු ගොස් නැවත උත්සාහ කරන්න.", "அந்தப் படிவம் காலாவதியானது. பின்சென்று மீண்டும் முயற்சிக்கவும்."},
	"somethingWrong":    {"Something went wrong. Please try again.", "යම් දෝෂයක් සිදු විය. නැවත උත්සාහ කරන්න.", "ஏதோ தவறு நேர்ந்தது. மீண்டும் முயற்சிக்கவும்."},
	"notFound":          {"There is no such page.", "එවැනි පිටුවක් නැත.", "அத்தகைய பக்கம் இல்லை."},

	"welcome":  {"Welcome", "ආයුබෝවන්", "வரவேற்கிறோம்"},
	"noRole":   {"You hold no role in ROSETTA. The Office grants roles to the people it names.", "ඔබට ROSETTA හි භූමිකාවක් නැත. කාර්යාලය භූමිකා ලබා දෙන්නේ එය නම් කරන අයටය.", "ROSETTA இல் உங்களுக்குப் பங்கு எதுவும் இல்லை. அலுவலகம் தான் பெயரிடுபவர்களுக்குப் பங்குகளை வழங்குகிறது."},
	"proposal": {"Any signed-in officer may propose a glossary term, once the glossary opens.", "පාරිභාෂික ශබ්දකෝෂය විවෘත වූ පසු, පිවිසුණු ඕනෑම නිලධාරියෙකුට පදයක් යෝජනා කළ හැකිය.", "சொற்களஞ்சியம் திறந்ததும், உள்நுழைந்த எந்த அலுவலரும் ஒரு சொல்லை முன்மொழியலாம்."},
}
