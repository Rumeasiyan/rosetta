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

	"welcome":    {"Welcome", "ආයුබෝවන්", "வரவேற்கிறோம்"},
	"noRole":     {"You hold no role in ROSETTA. The Office grants roles to the people it names.", "ඔබට ROSETTA හි භූමිකාවක් නැත. කාර්යාලය භූමිකා ලබා දෙන්නේ එය නම් කරන අයටය.", "ROSETTA இல் உங்களுக்குப் பங்கு எதுவும் இல்லை. அலுவலகம் தான் பெயரிடுபவர்களுக்குப் பங்குகளை வழங்குகிறது."},
	"yourRoles":  {"Your roles", "ඔබේ භූමිකා", "உங்கள் பங்குகள்"},
	"ownSystems": {"Systems you answer for", "ඔබ වගකියන පද්ධති", "நீங்கள் பொறுப்பேற்கும் முறைமைகள்"},
	"proposal":   {"Any signed-in officer may propose a glossary term, once the glossary opens.", "පාරිභාෂික ශබ්දකෝෂය විවෘත වූ පසු, පිවිසුණු ඕනෑම නිලධාරියෙකුට පදයක් යෝජනා කළ හැකිය.", "சொற்களஞ்சியம் திறந்ததும், உள்நுழைந்த எந்த அலுவலரும் ஒரு சொல்லை முன்மொழியலாம்."},

	"language": {"Language", "භාෂාව", "மொழி"},
	"menu":     {"Menu", "මෙනුව", "பட்டி"},

	"roleAdministrator":  {"Administrator", "පරිපාලක", "நிர்வாகி"},
	"roleGlossaryKeeper": {"Glossary keeper", "පාරිභාෂික භාරකරු", "சொற்களஞ்சியக் காப்பாளர்"},
	"roleMemoryReviewer": {"Memory reviewer", "මතක සමාලෝචක", "நினைவகச் சீராய்வாளர்"},
	"roleSystemOwner":    {"System owner", "පද්ධති හිමිකරු", "முறைமை உரிமையாளர்"},

	"notAllowed": {"Your role does not open this screen. This has been recorded.", "ඔබේ භූමිකාව මෙම තිරය විවෘත නොකරයි. මෙය වාර්තා කර ඇත.", "உங்கள் பங்கு இந்தத் திரையைத் திறக்காது. இது பதிவு செய்யப்பட்டுள்ளது."},

	"people":       {"People and roles", "පුද්ගලයන් සහ භූමිකා", "நபர்களும் பங்குகளும்"},
	"grantTitle":   {"Grant a role", "භූමිකාවක් ලබා දෙන්න", "பங்கை வழங்குக"},
	"personEmail":  {"The person's email", "පුද්ගලයාගේ විද්‍යුත් තැපෑල", "நபரின் மின்னஞ்சல்"},
	"grantNote":    {"They must have signed in once. System owner comes from the system register, not from here.", "ඔවුන් වරක් පිවිසී තිබිය යුතුය. පද්ධති හිමිකරු පැමිණෙන්නේ පද්ධති ලේඛනයෙනි, මෙතැනින් නොවේ.", "அவர் ஒருமுறை உள்நுழைந்திருக்க வேண்டும். முறைமை உரிமையாளர் முறைமைப் பதிவேட்டிலிருந்து வருகிறார், இங்கிருந்து அல்ல."},
	"person":       {"Person", "පුද්ගලයා", "நபர்"},
	"role":         {"Role", "භූමිකාව", "பங்கு"},
	"post":         {"Post", "තනතුර", "பதவி"},
	"from":         {"From", "සිට", "முதல்"},
	"until":        {"Until", "දක්වා", "வரை"},
	"noEnd":        {"No end date", "අවසන් දිනයක් නැත", "முடிவுத் திகதி இல்லை"},
	"noLanguage":   {"Not for one language", "එක් භාෂාවකට නොවේ", "ஒரு மொழிக்கு அல்ல"},
	"endsOn":       {"End date, if any", "අවසන් දිනය, තිබේ නම්", "முடிவுத் திகதி, இருந்தால்"},
	"grant":        {"Grant", "ලබා දෙන්න", "வழங்குக"},
	"end":          {"End", "අවසන් කරන්න", "முடிக்க"},
	"liveRoles":    {"Roles held now", "දැනට දරන භූමිකා", "இப்போது வகிக்கும் பங்குகள்"},
	"nobodyHolds":  {"Nobody holds a role.", "කිසිවෙකු භූමිකාවක් දරන්නේ නැත.", "யாரும் பங்கு வகிக்கவில்லை."},
	"postNotKnown": {"Not known", "නොදනී", "தெரியவில்லை"},
	"granted":      {"The role has been granted.", "භූමිකාව ලබා දී ඇත.", "பங்கு வழங்கப்பட்டது."},
	"ended":        {"The role has ended.", "භූමිකාව අවසන් විය.", "பங்கு முடிந்தது."},
	"badRole":      {"That role cannot be granted here.", "එම භූමිකාව මෙතැනින් ලබා දිය නොහැක.", "அந்தப் பங்கை இங்கு வழங்க முடியாது."},
	"badLanguage":  {"A glossary keeper or memory reviewer needs one language. An administrator has none.", "පාරිභාෂික භාරකරුට හෝ මතක සමාලෝචකට එක් භාෂාවක් අවශ්‍යයි. පරිපාලකට භාෂාවක් නැත.", "சொற்களஞ்சியக் காப்பாளருக்கும் நினைவகச் சீராய்வாளருக்கும் ஒரு மொழி தேவை. நிர்வாகிக்கு மொழி இல்லை."},
	"badDate":      {"The end date must be a date from today on.", "අවසන් දිනය අද සිට දිනයක් විය යුතුය.", "முடிவுத் திகதி இன்று முதலான திகதியாக இருக்க வேண்டும்."},
	"noSuchPerson": {"That person has not signed in.", "එම පුද්ගලයා පිවිසී නැත.", "அந்த நபர் உள்நுழையவில்லை."},
	"alreadyHeld":  {"That person holds this role already.", "එම පුද්ගලයා දැනටමත් මෙම භූමිකාව දරයි.", "அந்த நபர் ஏற்கனவே இந்தப் பங்கை வகிக்கிறார்."},
	"noSuchGrant":  {"That role is no longer held.", "එම භූමිකාව තවදුරටත් දරන්නේ නැත.", "அந்தப் பங்கு இனி வகிக்கப்படவில்லை."},
	"lastAdmin":    {"The last administrator cannot be ended. Grant another first.", "අවසන් පරිපාලක අවසන් කළ නොහැක. පළමුව තවත් අයෙකුට ලබා දෙන්න.", "கடைசி நிர்வாகியை முடிக்க முடியாது. முதலில் மற்றொருவருக்கு வழங்கவும்."},
}
