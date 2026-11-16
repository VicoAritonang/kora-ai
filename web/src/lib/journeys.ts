// Curated journeys: editorial story content for the landing and story pages.
// No prices live here on purpose — every number a traveller sees comes from
// Kora's Go package builder. A journey ends by handing its prompt to Kora.
import type { SceneVariant, SceneTime } from "@/components/scene/Scene";

export type JourneyChapter = {
  day: string;
  title: string;
  body: string;
  places: string[];
  time: SceneTime;
};

export type Journey = {
  slug: string;
  code: "OSA" | "UKY" | "TYO";
  city: string;
  scene: SceneVariant;
  kicker: string;
  title: string;
  tagline: string;
  nights: number;
  season: string;
  mood: string[];
  bestFor: string;
  chapters: JourneyChapter[];
  prompt: string;
};

export const journeys: Journey[] = [
  {
    slug: "osaka-dapur-jepang",
    code: "OSA",
    city: "Osaka",
    scene: "osaka",
    kicker: "Osaka · 5 malam",
    title: "Dapur Jepang yang Tak Pernah Tidur",
    tagline: "Takoyaki di Dotonbori, kastil berlapis emas, dan rusa yang membungkuk di Nara.",
    nights: 5,
    season: "Musim gugur",
    mood: ["Kuliner", "Kota", "Sejarah"],
    bestFor: "Pertama kali ke Jepang",
    chapters: [
      { day: "Hari 1", title: "Lampu neon Dotonbori", body: "Tiba di Kansai pagi hari, naik Nankai ke Namba, lalu biarkan papan reklame Glico menyambut. Malam pertama adalah soal makan: takoyaki, kushikatsu, dan jalan kaki di sepanjang kanal.", places: ["Dotonbori & Shinsaibashi", "Kuil Namba Yasaka", "Hozenji Yokocho"], time: "dusk" },
      { day: "Hari 2", title: "Kastil di tengah kota", body: "Pagi di taman Osaka-jo saat daun mulai merah. Naik ke menara utama, lalu lanjut ke Museum Sejarah Osaka untuk melihat kota dari atas.", places: ["Menara Utama Kastil Osaka", "Osaka Museum of History"], time: "day" },
      { day: "Hari 3", title: "Rusa dan Buddha raksasa", body: "Kereta Kintetsu 40 menit ke Nara. Todai-ji menyimpan Buddha perunggu setinggi 15 meter, dan rusa di tamannya lebih sopan dari kebanyakan orang.", places: ["Todai-ji & Taman Rusa Nara"], time: "dawn" },
      { day: "Hari 4", title: "Shinsekai yang retro", body: "Menara Tsutenkaku, gang-gang tua, lalu pemandangan kota dari Abeno Harukas saat matahari terbenam. Tutup dengan berendam di Spa World.", places: ["Menara Tsutenkaku & Shinsekai", "Abeno Harukas 300", "Spa World"], time: "night" },
    ],
    prompt: "Saya dari Jakarta, mau ke Osaka 5 malam musim gugur. Saya suka kuliner dan sejarah, ingin ke Nara juga. Tolong susun paketnya.",
  },
  {
    slug: "kyoto-seribu-gerbang",
    code: "UKY",
    city: "Kyoto",
    scene: "kyoto",
    kicker: "Kyoto · 4 malam",
    title: "Seribu Gerbang dan Dedaunan Merah",
    tagline: "Torii Fushimi Inari saat fajar, hutan bambu Arashiyama, dan lentera Gion di senja hari.",
    nights: 4,
    season: "Musim gugur",
    mood: ["Budaya", "Kuil", "Alam"],
    bestFor: "Pencinta foto & ketenangan",
    chapters: [
      { day: "Hari 1", title: "Fajar di Fushimi Inari", body: "Datang sebelum keramaian. Ribuan torii vermilion membentuk lorong yang mendaki gunung — semakin tinggi, semakin sepi.", places: ["Fushimi Inari Taisha", "Tofuku-ji (jembatan Tsutenkyo)"], time: "dawn" },
      { day: "Hari 2", title: "Higashiyama berjalan kaki", body: "Kiyomizu-dera di atas panggung kayunya, gang batu Ninenzaka, lalu Jalan Filsuf menuju Ginkaku-ji di bawah kanopi maple.", places: ["Kiyomizu-dera", "Ginkaku-ji & Jalan Filsuf", "Sanjusangen-do"], time: "day" },
      { day: "Hari 3", title: "Bambu yang berbisik", body: "Arashiyama di pagi hari: hutan bambu, taman Tenryu-ji, dan sungai Katsura. Sore hari, Pasar Nishiki untuk mencicipi acar dan mochi.", places: ["Hutan Bambu Arashiyama", "Taman Tenryu-ji", "Pasar Nishiki"], time: "day" },
      { day: "Hari 4", title: "Lentera Gion", body: "Saat senja, Hanamikoji menyalakan lenteranya. Jika beruntung, terlihat maiko bergegas ke ochaya. Malam ditutup dengan iluminasi Eikan-do.", places: ["Gion & Hanamikoji", "Eikan-do (iluminasi musim gugur)"], time: "night" },
    ],
    prompt: "Saya dari Jakarta, mau ke Kyoto 4 malam saat musim gugur untuk lihat dedaunan merah dan kuil. Tolong susun paketnya.",
  },
  {
    slug: "tokyo-kota-cahaya",
    code: "TYO",
    city: "Tokyo",
    scene: "tokyo",
    kicker: "Tokyo · 6 malam",
    title: "Kota Cahaya dari Asakusa ke Shibuya",
    tagline: "Kuil tertua kota, penyeberangan tersibuk dunia, dan seni digital yang membuat kaki basah.",
    nights: 6,
    season: "Musim semi",
    mood: ["Kota", "Seni", "Belanja"],
    bestFor: "Penjelajah kota",
    chapters: [
      { day: "Hari 1", title: "Asakusa yang klasik", body: "Gerbang Kaminarimon, jalan Nakamise yang penuh camilan, lalu Senso-ji. Sore hari naik Skytree dan lihat kota menyala satu per satu.", places: ["Senso-ji & Nakamise", "Tokyo Skytree"], time: "day" },
      { day: "Hari 2", title: "Hutan di tengah kota", body: "Meiji Jingu tersembunyi di balik pohon raksasa, lalu Harajuku yang berwarna dan Shinjuku Gyoen untuk piknik.", places: ["Meiji Jingu & Harajuku", "Shinjuku Gyoen"], time: "dawn" },
      { day: "Hari 3", title: "Seni yang bisa diinjak", body: "teamLab Planets di Toyosu: berjalan di air setinggi lutut dikelilingi cahaya. Siangnya sushi di pasar luar Tsukiji.", places: ["teamLab Planets", "Pasar luar Tsukiji"], time: "dusk" },
      { day: "Hari 4", title: "Shibuya dari atas", body: "Ribuan orang menyeberang serentak di Shibuya Crossing. Naik Shibuya Sky saat senja untuk melihatnya dari 230 meter.", places: ["Shibuya Sky", "Tokyo Tower"], time: "night" },
    ],
    prompt: "Saya dari Jakarta, mau ke Tokyo 6 malam musim semi. Suka kota, seni, dan belanja. Tolong susun paketnya.",
  },
  {
    slug: "osaka-keluarga",
    code: "OSA",
    city: "Osaka",
    scene: "osaka",
    kicker: "Osaka · 5 malam · keluarga",
    title: "Liburan Keluarga di Kansai",
    tagline: "Satu hari penuh di Universal Studios, akuarium raksasa Kaiyukan, dan membuat cup noodle sendiri.",
    nights: 5,
    season: "Liburan sekolah",
    mood: ["Keluarga", "Taman hiburan"],
    bestFor: "Keluarga dengan anak",
    chapters: [
      { day: "Hari 1", title: "Teluk Osaka", body: "Akuarium Kaiyukan dengan hiu paus di tangki tengahnya, lalu bianglala Tempozan saat matahari turun.", places: ["Akuarium Osaka Kaiyukan", "Bianglala Tempozan"], time: "dusk" },
      { day: "Hari 2", title: "Sehari di Universal", body: "Datang sebelum gerbang dibuka. Satu hari penuh wahana — anak-anak tidur nyenyak malam ini.", places: ["Universal Studios Japan"], time: "day" },
      { day: "Hari 3", title: "Cup noodle buatan sendiri", body: "Di Ikeda, setiap anak mendesain gelas dan meracik rasa mi instannya sendiri. Cocok juga saat hujan.", places: ["Cupnoodles Museum Ikeda"], time: "dawn" },
      { day: "Hari 4", title: "Rusa yang ramah", body: "Day trip ke Nara: memberi makan rusa dengan shika senbei, lalu Todai-ji yang megah.", places: ["Todai-ji & Taman Rusa Nara"], time: "night" },
    ],
    prompt: "Kami keluarga 2 dewasa dan 2 anak dari Jakarta, mau ke Osaka 5 malam saat liburan sekolah. Ingin ke Universal Studios dan akuarium. Tolong susun paketnya.",
  },
  {
    slug: "tokyo-hemat",
    code: "TYO",
    city: "Tokyo",
    scene: "tokyo",
    kicker: "Tokyo · 5 malam · hemat",
    title: "Tokyo dengan Dompet Ringan",
    tagline: "Kuil gratis, taman kota, dan pasar — Tokyo ternyata ramah untuk budget pas-pasan.",
    nights: 5,
    season: "Kapan saja",
    mood: ["Hemat", "Kota"],
    bestFor: "Backpacker",
    chapters: [
      { day: "Hari 1", title: "Asakusa tanpa tiket", body: "Senso-ji dan Nakamise tidak memungut biaya. Menginap di area Asakusa yang punya banyak hostel.", places: ["Senso-ji & Nakamise"], time: "day" },
      { day: "Hari 2", title: "Taman dan istana", body: "East Gardens Istana Kekaisaran gratis, Meiji Jingu gratis, dan Harajuku cukup untuk dilihat-lihat.", places: ["East Gardens Istana Kekaisaran", "Meiji Jingu & Harajuku"], time: "dawn" },
      { day: "Hari 3", title: "Museum dan Ueno", body: "Taman Ueno yang luas, lalu Tokyo National Museum untuk seni Jepang klasik.", places: ["Tokyo National Museum, Ueno"], time: "dusk" },
      { day: "Hari 4", title: "Lampu kota", body: "Tokyo Tower di malam hari terlihat paling cantik dari bawah — dan itu gratis.", places: ["Tokyo Tower"], time: "night" },
    ],
    prompt: "Saya dari Jakarta, punya budget 15 juta, mau ke Tokyo 5 malam se-hemat mungkin. Tolong susun paketnya.",
  },
  {
    slug: "kyoto-ryokan",
    code: "UKY",
    city: "Kyoto",
    scene: "kyoto",
    kicker: "Kyoto · 4 malam · nyaman",
    title: "Kyoto Pelan-Pelan dari Ryokan",
    tagline: "Bangun di ryokan Higashiyama, sarapan kaiseki, dan Paviliun Emas yang memantul di kolam.",
    nights: 4,
    season: "Musim gugur",
    mood: ["Nyaman", "Budaya"],
    bestFor: "Bulan madu",
    chapters: [
      { day: "Hari 1", title: "Paviliun Emas", body: "Kinkaku-ji memantul sempurna di kolam Kyoko-chi. Lanjutkan ke Kastil Nijo dengan lantai 'burung bulbul' yang berdecit.", places: ["Kinkaku-ji (Paviliun Emas)", "Kastil Nijo"], time: "day" },
      { day: "Hari 2", title: "Pagi di ryokan", body: "Futon, onsen pribadi, dan sarapan berbagai mangkuk kecil. Siangnya Kiyomizu-dera tanpa terburu-buru.", places: ["Kiyomizu-dera"], time: "dawn" },
      { day: "Hari 3", title: "Arashiyama di tepi sungai", body: "Menginap semalam di ryokan tepi sungai Arashiyama. Hutan bambu terasa berbeda saat sepi.", places: ["Hutan Bambu Arashiyama", "Taman Tenryu-ji"], time: "dusk" },
      { day: "Hari 4", title: "Gion yang tenang", body: "Jalan sore di Gion, makan malam di Pontocho dengan lentera di sepanjang gang.", places: ["Gion & Hanamikoji"], time: "night" },
    ],
    prompt: "Kami berdua dari Jakarta, mau ke Kyoto 4 malam musim gugur dengan nyaman, menginap di ryokan. Tolong susun paket yang nyaman.",
  },
];

export const journeyBySlug = (slug: string) => journeys.find((j) => j.slug === slug);

export const sceneFor = (code?: string | null): SceneVariant =>
  code === "OSA" ? "osaka" : code === "UKY" ? "kyoto" : code === "TYO" ? "tokyo" : "fuji";

// ------------------------------------------------------------- English --
type JourneyText = Pick<Journey, "kicker" | "title" | "tagline" | "season" | "mood" | "bestFor" | "prompt"> & {
  chapters: Pick<JourneyChapter, "day" | "title" | "body" | "places">[];
};

const english: Record<string, JourneyText> = {
  "osaka-dapur-jepang": {
    kicker: "Osaka · 5 nights",
    title: "Japan's Kitchen That Never Sleeps",
    tagline: "Takoyaki in Dotonbori, a gilded castle, and deer that bow back in Nara.",
    season: "Autumn",
    mood: ["Food", "City", "History"],
    bestFor: "First trip to Japan",
    chapters: [
      { day: "Day 1", title: "Dotonbori's neon", body: "Land in Kansai in the morning, ride the Nankai line to Namba and let the Glico sign welcome you. The first night is all about food: takoyaki, kushikatsu and a walk along the canal.", places: ["Dotonbori & Shinsaibashi", "Namba Yasaka Shrine", "Hozenji Yokocho"] },
      { day: "Day 2", title: "A castle in the city", body: "Morning in the Osaka Castle park as the leaves turn red. Climb the main keep, then head to the Osaka Museum of History for a view over the city.", places: ["Osaka Castle Main Tower", "Osaka Museum of History"] },
      { day: "Day 3", title: "Deer and a giant Buddha", body: "Forty minutes on the Kintetsu line to Nara. Todai-ji houses a 15-metre bronze Buddha, and the deer in its park are more polite than most people.", places: ["Todai-ji & Nara Deer Park"] },
      { day: "Day 4", title: "Retro Shinsekai", body: "Tsutenkaku Tower and old alleys, then the city from Abeno Harukas at sunset. End the day soaking at Spa World.", places: ["Tsutenkaku Tower & Shinsekai", "Abeno Harukas 300", "Spa World"] },
    ],
    prompt: "I'm from Jakarta and want to go to Osaka for 5 nights in autumn. I love food and history, and I'd like to visit Nara too. Please build the packages.",
  },
  "kyoto-seribu-gerbang": {
    kicker: "Kyoto · 4 nights",
    title: "A Thousand Gates and Red Leaves",
    tagline: "Fushimi Inari's torii at dawn, the Arashiyama bamboo grove, and Gion's lanterns at dusk.",
    season: "Autumn",
    mood: ["Culture", "Temples", "Nature"],
    bestFor: "Photo lovers & quiet seekers",
    chapters: [
      { day: "Day 1", title: "Dawn at Fushimi Inari", body: "Arrive before the crowds. Thousands of vermilion torii form a tunnel up the mountain — the higher you climb, the quieter it gets.", places: ["Fushimi Inari Taisha", "Tofuku-ji (Tsutenkyo Bridge)"] },
      { day: "Day 2", title: "Higashiyama on foot", body: "Kiyomizu-dera on its wooden stage, the stone lanes of Ninenzaka, then the Philosopher's Path to Ginkaku-ji beneath a maple canopy.", places: ["Kiyomizu-dera", "Ginkaku-ji & Philosopher's Path", "Sanjusangen-do"] },
      { day: "Day 3", title: "Whispering bamboo", body: "Arashiyama in the morning: the bamboo grove, Tenryu-ji's garden and the Katsura River. In the afternoon, Nishiki Market for pickles and mochi.", places: ["Arashiyama Bamboo Grove", "Tenryu-ji Garden", "Nishiki Market"] },
      { day: "Day 4", title: "Gion's lanterns", body: "At dusk, Hanamikoji lights its lanterns. With luck you'll see a maiko hurrying to a teahouse. The night ends with the illuminations at Eikan-do.", places: ["Gion & Hanamikoji", "Eikan-do (autumn illumination)"] },
    ],
    prompt: "I'm from Jakarta and want to go to Kyoto for 4 nights in autumn to see the red leaves and the temples. Please build the packages.",
  },
  "tokyo-kota-cahaya": {
    kicker: "Tokyo · 6 nights",
    title: "City of Light, from Asakusa to Shibuya",
    tagline: "The city's oldest temple, the world's busiest crossing, and digital art you wade through.",
    season: "Spring",
    mood: ["City", "Art", "Shopping"],
    bestFor: "City explorers",
    chapters: [
      { day: "Day 1", title: "Classic Asakusa", body: "Kaminarimon Gate, snack-lined Nakamise Street, then Senso-ji. In the afternoon, ride up the Skytree and watch the city light up one block at a time.", places: ["Senso-ji & Nakamise", "Tokyo Skytree"] },
      { day: "Day 2", title: "A forest in the city", body: "Meiji Jingu hides behind giant trees, then colourful Harajuku and a picnic in Shinjuku Gyoen.", places: ["Meiji Jingu & Harajuku", "Shinjuku Gyoen"] },
      { day: "Day 3", title: "Art you can walk through", body: "teamLab Planets in Toyosu: knee-deep water surrounded by light. Sushi for lunch at the Tsukiji outer market.", places: ["teamLab Planets", "Tsukiji Outer Market"] },
      { day: "Day 4", title: "Shibuya from above", body: "Thousands of people cross Shibuya Crossing at once. Go up Shibuya Sky at dusk and watch it from 230 metres.", places: ["Shibuya Sky", "Tokyo Tower"] },
    ],
    prompt: "I'm from Jakarta and want to go to Tokyo for 6 nights in spring. I like cities, art and shopping. Please build the packages.",
  },
  "osaka-keluarga": {
    kicker: "Osaka · 5 nights · family",
    title: "A Family Holiday in Kansai",
    tagline: "A full day at Universal Studios, the giant Kaiyukan aquarium, and making your own cup noodles.",
    season: "School holidays",
    mood: ["Family", "Theme parks"],
    bestFor: "Families with kids",
    chapters: [
      { day: "Day 1", title: "Osaka Bay", body: "The Kaiyukan Aquarium with a whale shark in its central tank, then the Tempozan Ferris Wheel as the sun goes down.", places: ["Osaka Aquarium Kaiyukan", "Tempozan Ferris Wheel"] },
      { day: "Day 2", title: "A day at Universal", body: "Arrive before the gates open. A full day of rides — the kids will sleep well tonight.", places: ["Universal Studios Japan"] },
      { day: "Day 3", title: "Homemade cup noodles", body: "In Ikeda, every child designs a cup and mixes their own noodle flavour. Perfect for a rainy day, too.", places: ["Cupnoodles Museum Ikeda"] },
      { day: "Day 4", title: "Friendly deer", body: "A day trip to Nara: feed the deer with shika senbei, then the grand Todai-ji.", places: ["Todai-ji & Nara Deer Park"] },
    ],
    prompt: "We're a family of 2 adults and 2 kids from Jakarta, going to Osaka for 5 nights during the school holidays. We want Universal Studios and the aquarium. Please build the packages.",
  },
  "tokyo-hemat": {
    kicker: "Tokyo · 5 nights · budget",
    title: "Tokyo on a Light Wallet",
    tagline: "Free temples, city parks and markets — Tokyo is kinder to tight budgets than you'd think.",
    season: "Any time",
    mood: ["Budget", "City"],
    bestFor: "Backpackers",
    chapters: [
      { day: "Day 1", title: "Asakusa, no ticket needed", body: "Senso-ji and Nakamise are free. Stay in Asakusa, which has plenty of hostels.", places: ["Senso-ji & Nakamise"] },
      { day: "Day 2", title: "Parks and palaces", body: "The Imperial Palace East Gardens are free, Meiji Jingu is free, and Harajuku is fun just to wander.", places: ["Imperial Palace East Gardens", "Meiji Jingu & Harajuku"] },
      { day: "Day 3", title: "Museums and Ueno", body: "The wide Ueno Park, then the Tokyo National Museum for classical Japanese art.", places: ["Tokyo National Museum, Ueno"] },
      { day: "Day 4", title: "City lights", body: "Tokyo Tower looks its best at night from below — and that's free.", places: ["Tokyo Tower"] },
    ],
    prompt: "I'm from Jakarta with a budget of IDR 15 million and want to go to Tokyo for 5 nights as cheaply as possible. Please build the packages.",
  },
  "kyoto-ryokan": {
    kicker: "Kyoto · 4 nights · comfort",
    title: "Slow Kyoto from a Ryokan",
    tagline: "Wake up in a Higashiyama ryokan, a kaiseki breakfast, and the Golden Pavilion mirrored in its pond.",
    season: "Autumn",
    mood: ["Comfort", "Culture"],
    bestFor: "Honeymoons",
    chapters: [
      { day: "Day 1", title: "The Golden Pavilion", body: "Kinkaku-ji reflects perfectly in the Kyoko-chi pond. Continue to Nijo Castle and its squeaking 'nightingale' floors.", places: ["Kinkaku-ji (Golden Pavilion)", "Nijo Castle"] },
      { day: "Day 2", title: "Morning at the ryokan", body: "Futons, a private onsen and a breakfast of many small bowls. Kiyomizu-dera in the afternoon, without hurrying.", places: ["Kiyomizu-dera"] },
      { day: "Day 3", title: "Arashiyama by the river", body: "One night in a riverside ryokan in Arashiyama. The bamboo grove feels different when it's quiet.", places: ["Arashiyama Bamboo Grove", "Tenryu-ji Garden"] },
      { day: "Day 4", title: "Quiet Gion", body: "An evening walk in Gion, then dinner in Pontocho with lanterns along the alley.", places: ["Gion & Hanamikoji"] },
    ],
    prompt: "The two of us are from Jakarta and want to go to Kyoto for 4 nights in autumn, comfortably, staying in a ryokan. Please build a comfortable package.",
  },
};

/** Journeys in the UI language (Indonesian is the source text). */
export function journeysFor(locale: "en" | "id"): Journey[] {
  if (locale === "id") return journeys;
  return journeys.map((j) => {
    const e = english[j.slug];
    if (!e) return j;
    return { ...j, ...e, chapters: j.chapters.map((c, i) => ({ ...c, ...e.chapters[i] })) };
  });
}
