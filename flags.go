package main

import (
	"regexp"
	"sort"
	"strings"
)

var countryAliases = func() map[string]string {
	result := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(countryNames), "\n") {
		parts := strings.Split(line, "|")
		for _, alias := range parts {
			result[strings.ToLower(alias)] = parts[0]
		}
	}
	result["uk"], result["usa"], result["uae"] = "GB", "US", "AE"
	return result
}()

var countryPattern = func() *regexp.Regexp {
	aliases := make([]string, 0, len(countryAliases))
	for alias := range countryAliases {
		aliases = append(aliases, regexp.QuoteMeta(alias))
	}
	sort.Slice(aliases, func(i, j int) bool {
		if len(aliases[i]) == len(aliases[j]) {
			return aliases[i] < aliases[j]
		}
		return len(aliases[i]) > len(aliases[j])
	})
	return regexp.MustCompile("(?i)" + strings.Join(aliases, "|"))
}()

func countryFlag(name string) string {
	for _, match := range countryPattern.FindAllStringIndex(name, -1) {
		alias := name[match[0]:match[1]]
		ascii := alias[0] < 128
		if ascii {
			if match[0] > 0 && ((name[match[0]-1] >= 'a' && name[match[0]-1] <= 'z') || (name[match[0]-1] >= 'A' && name[match[0]-1] <= 'Z')) {
				continue
			}
			if match[1] < len(name) && ((name[match[1]] >= 'a' && name[match[1]] <= 'z') || (name[match[1]] >= 'A' && name[match[1]] <= 'Z')) {
				continue
			}
			if len(alias) == 2 && alias != strings.ToUpper(alias) && !containsString([]string{"sg", "cn", "hk", "tw", "jp", "us", "kr", "de", "fr", "ru", "au", "ca", "nl", "uk"}, strings.ToLower(alias)) {
				continue
			}
			if strings.EqualFold(alias, "AI") || strings.EqualFold(alias, "SS") {
				continue
			}
		}
		code := countryAliases[strings.ToLower(alias)]
		return string([]rune{0x1f1e6 + rune(code[0]-'A'), 0x1f1e6 + rune(code[1]-'A')})
	}
	return ""
}

func flagNodeName(name string, origins []NodeOrigin) string {
	for _, char := range name {
		if char >= 0x1f1e6 && char <= 0x1f1ff {
			return name
		}
	}
	flag := countryFlag(name)
	if flag == "" {
		for _, origin := range origins {
			flag = countryFlag(origin.OriginalName)
			if flag != "" {
				break
			}
		}
	}
	if flag != "" {
		return flag + " " + name
	}
	return name
}

const countryNames = `
AD|Andorra|安道尔
AE|United Arab Emirates|Emirates|阿联酋|阿聯酋|迪拜|Dubai
AF|Afghanistan|阿富汗
AG|Antigua and Barbuda|安提瓜和巴布达
AI|Anguilla|安圭拉
AL|Albania|阿尔巴尼亚
AM|Armenia|亚美尼亚
AO|Angola|安哥拉
AQ|Antarctica|南极洲
AR|Argentina|阿根廷
AS|American Samoa|美属萨摩亚
AT|Austria|奥地利|奧地利
AU|Australia|澳大利亚|澳洲|澳大利亞|Sydney|悉尼
AW|Aruba|阿鲁巴
AX|Åland|奥兰群岛
AZ|Azerbaijan|阿塞拜疆
BA|Bosnia and Herzegovina|波黑|波斯尼亚
BB|Barbados|巴巴多斯
BD|Bangladesh|孟加拉
BE|Belgium|比利时|比利時
BF|Burkina Faso|布基纳法索
BG|Bulgaria|保加利亚
BH|Bahrain|巴林
BI|Burundi|布隆迪
BJ|Benin|贝宁
BL|Saint Barthélemy|圣巴泰勒米
BM|Bermuda|百慕大
BN|Brunei|文莱
BO|Bolivia|玻利维亚
BQ|Caribbean Netherlands|荷兰加勒比区
BR|Brazil|巴西
BS|Bahamas|巴哈马
BT|Bhutan|不丹
BV|Bouvet Island|布韦岛
BW|Botswana|博茨瓦纳
BY|Belarus|白俄罗斯|白俄羅斯
BZ|Belize|伯利兹
CA|Canada|加拿大|Toronto|多伦多|Vancouver|温哥华
CC|Cocos Islands|科科斯群岛
CD|Democratic Republic of the Congo|刚果金
CF|Central African Republic|中非
CG|Republic of the Congo|刚果布
CH|Switzerland|瑞士
CI|Ivory Coast|Côte d'Ivoire|科特迪瓦
CK|Cook Islands|库克群岛
CL|Chile|智利
CM|Cameroon|喀麦隆
CN|China|中国|中國|大陆|大陸|北京|上海|广州|深圳|Beijing|Shanghai|Guangzhou|Shenzhen
CO|Colombia|哥伦比亚
CR|Costa Rica|哥斯达黎加
CU|Cuba|古巴
CV|Cape Verde|Cabo Verde|佛得角
CW|Curaçao|Curacao|库拉索
CX|Christmas Island|圣诞岛
CY|Cyprus|塞浦路斯
CZ|Czech Republic|Czechia|捷克
DE|Germany|德国|德國|Frankfurt|法兰克福|法蘭克福
DJ|Djibouti|吉布提
DK|Denmark|丹麦|丹麥
DM|Dominica|多米尼克
DO|Dominican Republic|多米尼加
DZ|Algeria|阿尔及利亚
EC|Ecuador|厄瓜多尔
EE|Estonia|爱沙尼亚|愛沙尼亞
EG|Egypt|埃及
EH|Western Sahara|西撒哈拉
ER|Eritrea|厄立特里亚
ES|Spain|西班牙
ET|Ethiopia|埃塞俄比亚
FI|Finland|芬兰|芬蘭
FJ|Fiji|斐济
FK|Falkland Islands|福克兰群岛
FM|Micronesia|密克罗尼西亚
FO|Faroe Islands|法罗群岛
FR|France|法国|法國|Paris|巴黎
GA|Gabon|加蓬
GB|United Kingdom|Britain|England|英国|英國|London|伦敦|倫敦
GD|Grenada|格林纳达
GE|Georgia|格鲁吉亚
GF|French Guiana|法属圭亚那
GG|Guernsey|根西
GH|Ghana|加纳
GI|Gibraltar|直布罗陀
GL|Greenland|格陵兰
GM|Gambia|冈比亚
GN|Guinea|几内亚
GP|Guadeloupe|瓜德罗普
GQ|Equatorial Guinea|赤道几内亚
GR|Greece|希腊|希臘
GS|South Georgia and the South Sandwich Islands|南乔治亚
GT|Guatemala|危地马拉
GU|Guam|关岛|關島
GW|Guinea-Bissau|几内亚比绍
GY|Guyana|圭亚那
HK|Hong Kong|HongKong|香港|港区|港區
HM|Heard Island and McDonald Islands|赫德岛
HN|Honduras|洪都拉斯
HR|Croatia|克罗地亚
HT|Haiti|海地
HU|Hungary|匈牙利
ID|Indonesia|印尼|印度尼西亚|印度尼西亞
IE|Ireland|爱尔兰|愛爾蘭
IL|Israel|以色列
IM|Isle of Man|马恩岛
IN|India|印度|Mumbai|孟买
IO|British Indian Ocean Territory|英属印度洋领地
IQ|Iraq|伊拉克
IR|Iran|伊朗
IS|Iceland|冰岛|冰島
IT|Italy|意大利|義大利
JE|Jersey|泽西岛
JM|Jamaica|牙买加
JO|Jordan|约旦
JP|Japan|日本|东京|東京|大阪|Tokyo|Osaka
KE|Kenya|肯尼亚
KG|Kyrgyzstan|吉尔吉斯斯坦
KH|Cambodia|柬埔寨
KI|Kiribati|基里巴斯
KM|Comoros|科摩罗
KN|Saint Kitts and Nevis|圣基茨和尼维斯
KP|North Korea|朝鲜|朝鮮
KR|South Korea|Korea|韩国|韓國|南韩|南韓|首尔|首爾|Seoul
KW|Kuwait|科威特
KY|Cayman Islands|开曼群岛
KZ|Kazakhstan|哈萨克斯坦
LA|Laos|老挝|寮国|寮國
LB|Lebanon|黎巴嫩
LC|Saint Lucia|圣卢西亚
LI|Liechtenstein|列支敦士登
LK|Sri Lanka|斯里兰卡
LR|Liberia|利比里亚
LS|Lesotho|莱索托
LT|Lithuania|立陶宛
LU|Luxembourg|卢森堡|盧森堡
LV|Latvia|拉脱维亚
LY|Libya|利比亚
MA|Morocco|摩洛哥
MC|Monaco|摩纳哥
MD|Moldova|摩尔多瓦
ME|Montenegro|黑山
MF|Saint Martin|法属圣马丁
MG|Madagascar|马达加斯加
MH|Marshall Islands|马绍尔群岛
MK|North Macedonia|Macedonia|北马其顿
ML|Mali|马里
MM|Myanmar|Burma|缅甸|緬甸
MN|Mongolia|蒙古
MO|Macao|Macau|澳门|澳門
MP|Northern Mariana Islands|北马里亚纳群岛
MQ|Martinique|马提尼克
MR|Mauritania|毛里塔尼亚
MS|Montserrat|蒙特塞拉特
MT|Malta|马耳他
MU|Mauritius|毛里求斯|模里西斯
MV|Maldives|马尔代夫|馬爾代夫
MW|Malawi|马拉维
MX|Mexico|墨西哥
MY|Malaysia|马来西亚|馬來西亞|吉隆坡|Kuala Lumpur
MZ|Mozambique|莫桑比克
NA|Namibia|纳米比亚
NC|New Caledonia|新喀里多尼亚
NE|Niger|尼日尔
NF|Norfolk Island|诺福克岛
NG|Nigeria|尼日利亚
NI|Nicaragua|尼加拉瓜
NL|Netherlands|Holland|荷兰|荷蘭|Amsterdam|阿姆斯特丹
NO|Norway|挪威
NP|Nepal|尼泊尔
NR|Nauru|瑙鲁
NU|Niue|纽埃
NZ|New Zealand|新西兰|紐西蘭|新西蘭
OM|Oman|阿曼
PA|Panama|巴拿马
PE|Peru|秘鲁|秘魯
PF|French Polynesia|法属波利尼西亚
PG|Papua New Guinea|巴布亚新几内亚
PH|Philippines|菲律宾|菲律賓
PK|Pakistan|巴基斯坦
PL|Poland|波兰|波蘭
PM|Saint Pierre and Miquelon|圣皮埃尔
PN|Pitcairn|皮特凯恩
PR|Puerto Rico|波多黎各
PS|Palestine|巴勒斯坦
PT|Portugal|葡萄牙
PW|Palau|帕劳
PY|Paraguay|巴拉圭
QA|Qatar|卡塔尔|卡達
RE|Réunion|Reunion|留尼汪
RO|Romania|罗马尼亚|羅馬尼亞
RS|Serbia|塞尔维亚|塞爾維亞
RU|Russia|俄罗斯|俄羅斯|莫斯科|Moscow
RW|Rwanda|卢旺达
SA|Saudi Arabia|沙特阿拉伯|沙特
SB|Solomon Islands|所罗门群岛
SC|Seychelles|塞舌尔
SD|Sudan|苏丹
SE|Sweden|瑞典
SG|Singapore|新加坡|狮城|獅城
SH|Saint Helena|圣赫勒拿
SI|Slovenia|斯洛文尼亚
SJ|Svalbard and Jan Mayen|斯瓦尔巴
SK|Slovakia|斯洛伐克
SL|Sierra Leone|塞拉利昂
SM|San Marino|圣马力诺
SN|Senegal|塞内加尔
SO|Somalia|索马里
SR|Suriname|苏里南
SS|South Sudan|南苏丹
ST|São Tomé and Príncipe|圣多美和普林西比
SV|El Salvador|萨尔瓦多
SX|Sint Maarten|荷属圣马丁
SY|Syria|叙利亚
SZ|Eswatini|Swaziland|斯威士兰
TC|Turks and Caicos Islands|特克斯和凯科斯
TD|Chad|乍得
TF|French Southern Territories|法属南部领地
TG|Togo|多哥
TH|Thailand|泰国|泰國|Bangkok|曼谷
TJ|Tajikistan|塔吉克斯坦
TK|Tokelau|托克劳
TL|Timor-Leste|East Timor|东帝汶
TM|Turkmenistan|土库曼斯坦
TN|Tunisia|突尼斯
TO|Tonga|汤加
TR|Turkey|Türkiye|土耳其
TT|Trinidad and Tobago|特立尼达和多巴哥
TV|Tuvalu|图瓦卢
TW|Taiwan|台湾|台灣|臺灣|台北|臺北|Taipei
TZ|Tanzania|坦桑尼亚
UA|Ukraine|乌克兰|烏克蘭
UG|Uganda|乌干达
UM|United States Minor Outlying Islands|美国本土外小岛屿
US|United States|United States of America|America|美国|美國|洛杉矶|洛杉磯|Los Angeles|San Jose|圣何塞|New York|纽约|紐約|Seattle|西雅图|西雅圖
UY|Uruguay|乌拉圭
UZ|Uzbekistan|乌兹别克斯坦
VA|Vatican|Holy See|梵蒂冈
VC|Saint Vincent and the Grenadines|圣文森特和格林纳丁斯
VE|Venezuela|委内瑞拉
VG|British Virgin Islands|英属维尔京群岛
VI|US Virgin Islands|美属维尔京群岛
VN|Vietnam|越南
VU|Vanuatu|瓦努阿图
WF|Wallis and Futuna|瓦利斯和富图纳
WS|Samoa|萨摩亚
YE|Yemen|也门
YT|Mayotte|马约特
ZA|South Africa|南非
ZM|Zambia|赞比亚
ZW|Zimbabwe|津巴布韦
`
