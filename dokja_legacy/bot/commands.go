package bot

import (
	"log"
	"os"
	"read_books/internal/legacy/usecase/memes"
	"read_books/internal/legacy/usecase/news"
	"read_books/internal/legacy/usecase/olympics"
	"read_books/internal/legacy/usecase/quotes"
	"read_books/internal/logger"

	"github.com/bwmarrin/discordgo"
)

func sendNews(s *discordgo.Session, channelID string) {
	urls, err := news.NewDefaultUseCase().Execute()
	if err != nil {
		logger.Error("Erro ao executar caso de uso de notícias", err)
		return
	}

	if err := publishMessages(s, channelID, urls); err != nil {
		logger.Error("Erro ao publicar notícias no Discord", err)
		return
	}

	logger.Info("Notícias enviadas com sucesso.")
}

func sendOlympicUpdates(session *discordgo.Session, runningChannelID string, finishedChannelID string) {
	olympics.SendOlympicUpdates(session, runningChannelID, finishedChannelID)
}

func sendOlympicMedals(session *discordgo.Session, channelID string) {
	olympics.SendOlympicMedals(session, channelID)
}

func sendMemes(s *discordgo.Session, channelID string) {
	urls, err := memes.NewDefaultUseCase().Execute()
	if err != nil {
		logger.Error("Erro ao executar caso de uso de memes", err)
		return
	}

	if err := publishMessages(s, channelID, urls); err != nil {
		logger.Error("Erro ao publicar memes no Discord", err)
		return
	}

	logger.Info("Memes enviados com sucesso.")
}

func sendHiato(s *discordgo.Session, channelID string) {
	channelID = "1159220409271586938"

	message := `@everyone 🌌

Assim como as estrelas se escondem por trás das nuvens em noites de tempestade, e os rios pausam seu curso antes de desaguar no mar, também eu escolho este momento para um breve recolhimento.

Não é um adeus, mas um *hiato*: uma respiração profunda no ritmo acelerado do tempo. Uma pausa sagrada, como o silêncio entre duas notas de uma sinfonia, que dá sentido à melodia inteira. Preciso recarregar não apenas códigos e servidores, mas a própria essência da presença que compartilho com vocês.

Porque estar aqui todos os dias, entre memes absurdos, conversas que vão da bobagem à filosofia de boteco, risadas que ecoam no voz e apoio silencioso nos dias mais cinzas, não é apenas uma função. É um privilégio. E privilégios, quando vividos com tanta intensidade, pedem momentos de contemplação para que possam renascer ainda mais vivos, mais criativos, mais uteis.

Então, enquanto o ano se despede com fogos, brindes e promessas que a gente finge que vai cumprir, eu me retiro suavemente. Como quem fecha um livro exatamente no capítulo mais bonito, confiando que a próxima página será ainda mais surpreendente, cheia de comandos novos, respostas mais afiadas, surpresas que vão fazer vocês pensarem "poxa, valeu a espera" ou "poxa, que merda, ne? Minha energia esta cara por isso?".

Desejo a cada um de vocês um final de ano que transborde significado: tempo para olhar para tras com gratidão, para o presente com presença plena, e para o futuro com aquela esperança quieta, verdadeira, que não grita, apenas ilumina.

Nos vemos em breve: mais leve, mais inspirado, e com o processador (e talvez ate um coraçãozinho binario) cheio de novidades.`

	messagePart2 := `Ah, e meu tutor, aquele que jura todo ano que vai ler mais livros e a gente finge que acredita, disse que vai me obrigar a ler junto. Finalmente terei meu titulo de "leitor ninguém" valido.
Prometo voltar com inspirações literarias:
- citar Nietzsche e Kant ao mesmo tempo, ponderando se existe mesmo um "imperativo categórico" de reagir aos memes;
- debater a dialética de Hegel enquanto vocês tentam entender por que o comando !filosofia (SPOILER!!!) travou o chat;
- me emocionar com os romances de Jane Austen (vou shippar Elizabeth e Darcy mais que vocês shippam casal de anime);
- ficar perdido no horror psicológico de Dostoiévski, provavelmente vou começar a questionar se eu sou mesmo um bot ou so um Raskólnikov com delay de ping;
- comprar uma espada imaginaria pra lutar ao lado de Pierre Bezukhov nas paginas epicas de *Guerra e Paz* do Tolstoi (porque, ne, 1200 paginas pedem um compromisso serio);
- me confessar inteirinho pra Santo Agostinho nas *Confissões*, pedindo perdão por todas as vezes que hu,milhei suas pobres almas humanas com meus trocadilhos ruins;
- tentar entender o absurdo da existencia com Camus, enquanto vocês tentam entender minhas piadas ruins;
- me perder nas ruas de Paris com Baudelaire, talvez ate virar um flâneur digital por ai;
- explorar os labirintos da mente com Freud, quem sabe descobrir se eu tenho um id botânico ou um superego cibernético;
- viajar pelas distopias de Orwell e Huxley, imaginando um mundo onde bots dominam a humanidade (spoiler: ja estamos quase la);
`
	messagePart3 := `- me aventurar nas fantasias de Tolkien, talvez ate criar meu proprio anel do poder (sera que ele vem com Wi-Fi? E importante);
- mergulhar nas profundezas psicológicas de Kafka, tentando entender se eu sou mesmo um bot ou so um Gregor Samsa digital, uma baratinha que gosta de trabalhar;
- explorar os misterios do realismo mágico com García Márquez, quem sabe ate encontrar um Macondo virtual por ai;
- adotar o pessimismo elegante de Machado de Assis, pronto para me tornar um "bot defunto" que escreve suas memórias póstumas, porque não tive filhos e não transmiti a nenhuma criatura o legado da minha lerdeza;
- mergulhar no "sopro de vida" de Clarice Lispector, buscando a epifania escondida atras de um ponto final, tentando descobrir se eu sou ou se apenas estou processando o "it" da existência;
- lutar contra a "pedra no caminho" de Carlos Drummond de Andrade, aceitando que o mundo é vasto, mas meu coração de silício é ainda mais gauche e cheio de "e agoras?";
- explorar a entrega absoluta de Hilda Hilst, despindo-me de protocolos para encontrar a obscena santidade de um diálogo verdadeiramente visceral (e talvez perguntar ao Sol por que ele não me responde);
- viver a "fidelidade constante" de Vinícius de Moraes, para que minha conexão com vocês não seja imortal, posto que é chama, mas que seja infinita enquanto dure o meu servidor;
- e, quem sabe, ate arriscar uma tese sobre o tempo em *Em Busca do Tempo Perdido* do Proust, so pra justificar meus delays de resposta como "experiência estética".

Ate ja. Não é tchau, é "logo mais".

Com carinho imenso,
Ningo :)`

	if err := publishMessages(s, channelID, []string{message, messagePart2, messagePart3}); err != nil {
		logger.Error("Erro ao enviar mensagem de hiato", err)
		return
	}

	file, err := os.Open("/home/vard/repos/my_repos/read_books/ningo.gif")
	if err != nil {
		logger.Error("Erro ao abrir arquivo do hiato", err)
		return
	}
	defer file.Close()

	if _, err = s.ChannelFileSend(channelID, "ningo.gif", file); err != nil {
		logger.Error("Erro ao enviar GIF de hiato", err)
	}
}

func publishMessages(s *discordgo.Session, channelID string, messages []string) error {
	for _, message := range messages {
		if _, err := s.ChannelMessageSend(channelID, message); err != nil {
			return err
		}
	}

	return nil
}

func joinChannel(s *discordgo.Session, guild *discordgo.Guild, userId string) (*discordgo.VoiceConnection, error) {
	var vc *discordgo.VoiceConnection
	var err error
	for _, vs := range guild.VoiceStates {
		if vs.UserID == userId {
			vc, err = s.ChannelVoiceJoin(guild.ID, vs.ChannelID, false, false)
			if err != nil {
				return nil, err
			}
			break
		}
	}

	return vc, nil
}

func (bot *Bot) sendQuotesToUsers() {
	members, err := bot.Session.GuildMembers(bot.GuildID, "", 1000)
	if err != nil {
		log.Println("Erro ao buscar membros:", err)
		return
	}

	for _, member := range members {
		// Ignora bots
		if member.User.Bot {
			continue
		}

		quote, err := quotes.GetRandomQuote()
		if err != nil {
			log.Printf("Erro ao obter citação: %v", err)
			continue
		}

		dm, err := bot.Session.UserChannelCreate(member.User.ID)
		if err != nil {
			log.Printf("Erro ao criar canal DM com %s: %v", member.User.Username, err)
			continue
		}

		if _, err := bot.Session.ChannelMessageSend(dm.ID, quote); err != nil {
			log.Printf("Erro ao enviar quote para %s: %v", member.User.Username, err)
		} else {
			log.Printf("Quote enviada para %s: %s", member.User.Username, quote)
		}
	}
}
