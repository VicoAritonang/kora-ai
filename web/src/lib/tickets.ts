// Sample board for the tickets page: mirrors the base table of the Go sandbox
// supplier (internal/adapters/sandbox). These are simulated reference fares,
// never offers — real (sandbox or live) prices only come from a Kora run.

export type SampleFlight = {
  code: string;
  carrier: string;
  from: string;
  to: string;
  via: string | null;
  depart: string; // local
  arrive: string; // local at destination
  nextDay: boolean;
  minutes: number; // total travel time, one way
  baggageKg: number;
  baseIdr: number; // round trip, 1 adult, off-peak (sandbox base)
};

export const sampleFlights: SampleFlight[] = [
  { code: "TR", carrier: "Scoot", from: "CGK", to: "KIX", via: "SIN", depart: "01:00", arrive: "16:10", nextDay: false, minutes: 790, baggageKg: 0, baseIdr: 4_550_000 },
  { code: "D7", carrier: "AirAsia X", from: "CGK", to: "KIX", via: "KUL", depart: "08:00", arrive: "01:00", nextDay: true, minutes: 900, baggageKg: 0, baseIdr: 4_850_000 },
  { code: "CX", carrier: "Cathay Pacific", from: "CGK", to: "KIX", via: "HKG", depart: "07:00", arrive: "22:55", nextDay: false, minutes: 835, baggageKg: 23, baseIdr: 7_100_000 },
  { code: "GA", carrier: "Garuda Indonesia", from: "CGK", to: "KIX", via: null, depart: "22:00", arrive: "07:25", nextDay: true, minutes: 445, baggageKg: 30, baseIdr: 7_650_000 },
  { code: "SQ", carrier: "Singapore Airlines", from: "CGK", to: "KIX", via: "SIN", depart: "06:00", arrive: "20:40", nextDay: false, minutes: 760, baggageKg: 30, baseIdr: 8_200_000 },
  { code: "JL", carrier: "Japan Airlines", from: "CGK", to: "KIX", via: null, depart: "21:00", arrive: "06:10", nextDay: true, minutes: 430, baggageKg: 46, baseIdr: 9_350_000 },
  { code: "NH", carrier: "ANA", from: "CGK", to: "KIX", via: null, depart: "22:00", arrive: "07:30", nextDay: true, minutes: 450, baggageKg: 46, baseIdr: 9_900_000 },
];

export type SampleStay = { name: string; city: "OSA" | "UKY" | "TYO"; area: string; stars: number; rating: number; stationM: number; nightlyJpy: number };

export const sampleStays: SampleStay[] = [
  { name: "Kora Hostel Namba", city: "OSA", area: "Namba", stars: 2, rating: 8.3, stationM: 250, nightlyJpy: 4200 },
  { name: "Dotonbori Riverside Inn", city: "OSA", area: "Namba", stars: 3, rating: 8.0, stationM: 400, nightlyJpy: 8500 },
  { name: "Umeda Business Hotel", city: "OSA", area: "Umeda", stars: 3, rating: 8.4, stationM: 300, nightlyJpy: 10500 },
  { name: "Shinsaibashi Boutique Hotel", city: "OSA", area: "Shinsaibashi", stars: 4, rating: 8.7, stationM: 350, nightlyJpy: 16000 },
  { name: "Grand Umeda Tower Hotel", city: "OSA", area: "Umeda", stars: 5, rating: 9.2, stationM: 200, nightlyJpy: 38000 },
  { name: "Kyoto Station Hostel", city: "UKY", area: "Kyoto Station", stars: 2, rating: 8.2, stationM: 300, nightlyJpy: 4800 },
  { name: "Gion Machiya Guesthouse", city: "UKY", area: "Gion", stars: 3, rating: 8.8, stationM: 700, nightlyJpy: 12500 },
  { name: "Higashiyama Ryokan", city: "UKY", area: "Higashiyama", stars: 4, rating: 9.0, stationM: 900, nightlyJpy: 26000 },
  { name: "Arashiyama Riverside Ryokan", city: "UKY", area: "Arashiyama", stars: 4, rating: 9.1, stationM: 500, nightlyJpy: 32000 },
  { name: "Asakusa Backpackers", city: "TYO", area: "Asakusa", stars: 2, rating: 8.4, stationM: 200, nightlyJpy: 4500 },
  { name: "Shinjuku Business Hotel", city: "TYO", area: "Shinjuku", stars: 3, rating: 8.2, stationM: 300, nightlyJpy: 13000 },
  { name: "Shibuya Design Hotel", city: "TYO", area: "Shibuya", stars: 4, rating: 8.8, stationM: 400, nightlyJpy: 21000 },
  { name: "Grand Marunouchi Hotel", city: "TYO", area: "Marunouchi", stars: 5, rating: 9.3, stationM: 200, nightlyJpy: 52000 },
];

export const cityLabel: Record<string, string> = { OSA: "Osaka", UKY: "Kyoto", TYO: "Tokyo" };
